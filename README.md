# kubeconfig-updater

**kubeconfig-updater** is a command‑line tool written in Go that helps you update your Kubernetes kubeconfig file with new certificate and user token information. It provides an interactive prompt interface (powered by [huh](https://github.com/charmbracelet/huh/)) to guide you through selecting an existing context or creating a new one, and then updating the associated cluster and user data from a pasted kubeconfig file.

> **Note:** The tool creates a backup of your current kubeconfig (appending a `.backup.YYYYMMDD` extension) before making any changes.

## Features

- **Interactive Prompts:** Choose or create a context using user-friendly prompts.
- **Selective Updates:** Only update the selected cluster and its associated user from a pasted kubeconfig (even if the pasted file contains multiple clusters/users).
- **Automatic Backup:** A backup of your original kubeconfig is created before applying updates.
- **Change Summary:** Prints a concise summary of changes (showing only the first and last few characters of sensitive data).
- **Try Mode:** Use the `--try` flag to preview changes without modifying your kubeconfig file.

## Usage

Run the tool using the default kubeconfig file (~/.kube/config):

```bash
  ./kubeconfig-updater
```

To specify a different kubeconfig file, use the `--config` flag:

```bash
  ./kubeconfig-updater --config=/path/to/kubeconfig
```

To preview changes without updating the file, use the --try flag:

```bash
  ./kubeconfig-updater --try
```

## Example

After running the tool, you might see output similar to:

```bash
Summary of changes:
- Updated cluster "prod-cluster" server from "https://abcde...vwxyz" to "https://12345...67890"
- Updated user "prod-user" token from "token1...token2" to "token3...token4"
```

A backup of your original kubeconfig will be saved as ~/.kube/config.backup.YYYYMMDD before any modifications are applied.

