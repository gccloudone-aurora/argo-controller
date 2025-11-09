package cmd

import (
	"context"
	"encoding/json"
	"time"

	argov1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	argoclientset "github.com/argoproj/argo-cd/v2/pkg/client/clientset/versioned"
	argoinformers "github.com/argoproj/argo-cd/v2/pkg/client/informers/externalversions"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	"github.com/gccloudone-aurora/argo-controller/pkg/signals"
)

const (
	sourceNamespace = "platform-management-system"
	targetNamespace = "platform-solution-system"
	labelKey        = "appproject"
	labelValue      = "solution"
)

var syncAppProjectsCmd = &cobra.Command{
	Use:   "sync-appprojects",
	Short: "Sync AppProjects between platform-management-system and platform-solution-system",
	Long:  "Sync AppProjects between platform-management-system and platform-solution-system",
	Run: func(cmd *cobra.Command, args []string) {
		// Setup signals so we can shutdown cleanly
		stopCh := signals.SetupSignalHandler()

		// Create Kubernetes config
		cfg, err := clientcmd.BuildConfigFromFlags(apiserver, kubeconfig)
		if err != nil {
			klog.Fatalf("Error building kubeconfig: %v", err)
		}

		argoClient, err := argoclientset.NewForConfig(cfg)
		if err != nil {
			klog.Fatalf("Error creating Argo client: %v", err)
		}

		kubeClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			klog.Fatalf("Error creating Kubernetes client: %v", err)
		}

		// Setup informers
		informerFactory := argoinformers.NewSharedInformerFactoryWithOptions(
			argoClient,
			time.Minute*5,
			argoinformers.WithNamespace(sourceNamespace),
		)

		projectInformer := informerFactory.Argoproj().V1alpha1().AppProjects().Informer()

		projectInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				handleSync(obj, argoClient, kubeClient)
			},
			UpdateFunc: func(_, newObj interface{}) {
				handleSync(newObj, argoClient, kubeClient)
			},
			DeleteFunc: func(obj interface{}) {
				handleDelete(obj, argoClient)
			},
		})

		// Start informers
		informerFactory.Start(stopCh)

		// Wait for caches
		klog.Info("Waiting for AppProject caches to sync...")
		if ok := cache.WaitForCacheSync(stopCh, projectInformer.HasSynced); !ok {
			klog.Fatalf("Failed to sync caches")
		}

		<-stopCh
	},
}

func handleSync(obj interface{}, argoClient argoclientset.Interface, kubeClient *kubernetes.Clientset) {
	project, ok := obj.(*argov1.AppProject)
	if !ok {
		klog.Warning("Unexpected object type")
		return
	}

	if project.Labels[labelKey] != labelValue {
		return
	}

	srcNs := sourceNamespace
	tgtNs := targetNamespace

	srcNsObj, err1 := kubeClient.CoreV1().Namespaces().Get(context.Background(), srcNs, metav1.GetOptions{})
	tgtNsObj, err2 := kubeClient.CoreV1().Namespaces().Get(context.Background(), tgtNs, metav1.GetOptions{})
	if err1 != nil || err2 != nil || srcNsObj == nil || tgtNsObj == nil {
		klog.Warningf("Skipping AppProject %s — namespace(s) missing (src exists=%t, tgt exists=%t)",
			project.Name, err1 == nil, err2 == nil)
		return
	}

	data, _ := json.Marshal(project)
	var copy argov1.AppProject
	_ = json.Unmarshal(data, &copy)

	copy.Namespace = tgtNs
	copy.ResourceVersion = ""
	copy.UID = ""
	copy.ManagedFields = nil
	copy.Annotations = nil

	if copy.Labels == nil {
		copy.Labels = map[string]string{}
	}
	copy.Labels["mirrored-from"] = srcNs
	copy.Labels["mirrored-by"] = "gccloudone-aurora/argo-controller"
	copy.Labels["mirrored-timestamp"] = time.Now().UTC().Format(time.RFC3339)

	klog.Infof("Syncing AppProject %s from %s → %s", project.Name, srcNs, tgtNs)

	existing, err := argoClient.ArgoprojV1alpha1().AppProjects(tgtNs).Get(context.Background(), copy.Name, metav1.GetOptions{})
	if err == nil {
		copy.ResourceVersion = existing.ResourceVersion
		if _, err := argoClient.ArgoprojV1alpha1().AppProjects(tgtNs).Update(context.Background(), &copy, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("Error updating AppProject %s: %v", project.Name, err)
		}
	} else if apierrors.IsNotFound(err) {
		if _, err := argoClient.ArgoprojV1alpha1().AppProjects(tgtNs).Create(context.Background(), &copy, metav1.CreateOptions{}); err != nil {
			klog.Errorf("Error creating AppProject %s: %v", project.Name, err)
		}
	} else {
		klog.Errorf("Error getting AppProject %s: %v", project.Name, err)
	}
}

func handleDelete(obj interface{}, argoClient argoclientset.Interface) {
	project, ok := obj.(*argov1.AppProject)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Warning("Unexpected delete object type")
			return
		}
		project, ok = tombstone.Obj.(*argov1.AppProject)
		if !ok {
			klog.Warning("Tombstone object is not an AppProject")
			return
		}
	}

	if project.Labels[labelKey] != labelValue {
		return
	}

	err := argoClient.ArgoprojV1alpha1().AppProjects(targetNamespace).
		Delete(context.Background(), project.Name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("Error deleting mirrored AppProject %s: %v", project.Name, err)
	} else {
		klog.Infof("Deleted mirrored AppProject %s from %s", project.Name, targetNamespace)
	}
}

// --- Init --------------------------------------------------------------------

func init() {
	rootCmd.AddCommand(syncAppProjectsCmd)
}
