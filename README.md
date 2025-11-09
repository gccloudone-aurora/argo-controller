# argo-controller

A collection of lightweight controllers that extend and improve integration with **Argo CD** across Aurora environments.

These controllers perform focused automation tasks that are not handled by upstream Argo CD, helping enforce platform governance and simplify operations within multi-instance deployment.

---

## 🧠 Overview

| Controller             | Purpose                                                                                                                                                       |
|------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **image-pull-secrets** | Ensures all Argo-related ServiceAccounts have access to the platform’s shared container registry secret.                                                      |
| **sync-appprojects**   | Mirrors `AppProjects` labeled as `appproject: solution` between the platform and solution namespaces, keeping governance consistent across Argo CD instances. |
| **workflows**          | Handles Argo Workflows role binding setup and propagates storage account credentials to workflow components.                                                  |

Each controller runs as a separate Deployment within the same Helm chart. Controllers can be individually enabled or disabled in `values.yaml`.
