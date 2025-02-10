package cmd

import (
	"context"
	"time"

	"github.com/gccloudone-aurora/argo-controller/pkg/controllers/serviceaccounts"
	"github.com/gccloudone-aurora/argo-controller/pkg/signals"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

var imagePullSecretName string

var imagePullSecretsCmd = &cobra.Command{
	Use:   "image-pull-secrets",
	Short: "Configure image pull secrets for Argo resources",
	Long:  `Configure image pull secrets for Argo resources`,
	Run: func(cmd *cobra.Command, args []string) {
		// Setup signals so we can shutdown cleanly
		stopCh := signals.SetupSignalHandler()

		// Create Kubernetes config
		cfg, err := clientcmd.BuildConfigFromFlags(apiserver, kubeconfig)
		if err != nil {
			klog.Fatalf("error building kubeconfig: %v", err)
		}

		kubeClient, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
		}

		// Setup informers
		kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Minute*5)

		// Serviceaccount informer
		serviceAccountsInformer := kubeInformerFactory.Core().V1().ServiceAccounts()
		// serviceAccountsLister := serviceAccountsInformer.Lister()

		// Setup controller
		controller := serviceaccounts.NewController(
			serviceAccountsInformer,
			func(serviceAccount *corev1.ServiceAccount) error {
				if val, ok := serviceAccount.Labels["app.kubernetes.io/part-of"]; ok && val == "argocd" {
					found := false
					for _, imagePullSecret := range serviceAccount.ImagePullSecrets {
						if imagePullSecret.Name == imagePullSecretName {
							found = true
							break
						}
					}

					if !found {
						klog.Infof("Adding image pull secret to %s/%s", serviceAccount.Namespace, serviceAccount.Name)

						// Add the image pull secret
						updated := serviceAccount.DeepCopy()
						updated.ImagePullSecrets = append(serviceAccount.ImagePullSecrets, corev1.LocalObjectReference{Name: imagePullSecretName})
						if _, err := kubeClient.CoreV1().ServiceAccounts(serviceAccount.Namespace).Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
							return err
						}
					}
				}

				return nil
			},
		)

		serviceAccountsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: controller.HandleObject,
			UpdateFunc: func(old, new interface{}) {
				newNP := new.(*corev1.ServiceAccount)
				oldNP := old.(*corev1.ServiceAccount)

				if newNP.ResourceVersion == oldNP.ResourceVersion {
					return
				}

				controller.HandleObject(new)
			},
		})

		// Start informers
		kubeInformerFactory.Start(stopCh)

		// Wait for caches
		klog.Info("Waiting for informer caches to sync")
		if ok := cache.WaitForCacheSync(stopCh, serviceAccountsInformer.Informer().HasSynced); !ok {
			klog.Fatalf("failed to wait for caches to sync")
		}

		// Run the controller
		if err = controller.Run(2, stopCh); err != nil {
			klog.Fatalf("error running controller: %v", err)
		}
	},
}

func init() {
	imagePullSecretsCmd.Flags().StringVar(&imagePullSecretName, "image-pull-secret", "image-pull-secret", "Name of the secret containing the image pull credentials.")

	rootCmd.AddCommand(imagePullSecretsCmd)
}
