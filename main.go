package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/charmbracelet/huh"
)

// KubeConfig defines a minimal structure for kubeconfig files.
type KubeConfig struct {
	APIVersion     string         `yaml:"apiVersion"`
	Kind           string         `yaml:"kind"`
	Clusters       []NamedCluster `yaml:"clusters"`
	Contexts       []NamedContext `yaml:"contexts"`
	Users          []NamedUser    `yaml:"users"`
	CurrentContext string         `yaml:"current-context"`
}

type NamedCluster struct {
	Name    string  `yaml:"name"`
	Cluster Cluster `yaml:"cluster"`
}

type Cluster struct {
	Server                   string `yaml:"server"`
	CertificateAuthorityData string `yaml:"certificate-authority-data,omitempty"`
}

type NamedContext struct {
	Name    string  `yaml:"name"`
	Context Context `yaml:"context"`
}

type Context struct {
	Cluster string `yaml:"cluster"`
	User    string `yaml:"user"`
}

type NamedUser struct {
	Name string `yaml:"name"`
	User User   `yaml:"user"`
}

type User struct {
	Token                 string `yaml:"token,omitempty"`
	ClientCertificateData string `yaml:"client-certificate-data,omitempty"`
	ClientKeyData         string `yaml:"client-key-data,omitempty"`
}

// shorten returns a truncated version of a secret string: first 5 and last 5 characters.
func shorten(s string) string {
	if len(s) <= 15 {
		return s
	}
	return fmt.Sprintf("%s...%s", s[:5], s[len(s)-5:])
}

func main() {
	// Define command-line flags.
	configPathFlag := flag.String("config", "~/.kube/config", "Path to kubeconfig file")
	tryFlag := flag.Bool("try", false, "Try mode: do not update file, just print output")
	flag.Parse()

	// Expand "~" in the config path.
	configPath := *configPathFlag
	if strings.HasPrefix(configPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting home directory: %v\n", err)
			os.Exit(1)
		}
		configPath = filepath.Join(home, configPath[1:])
	}

	// Read the existing kubeconfig.
	origData, err := ioutil.ReadFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading kubeconfig file %s: %v\n", configPath, err)
		os.Exit(1)
	}
	var origCfg KubeConfig
	if err := yaml.Unmarshal(origData, &origCfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing kubeconfig file: %v\n", err)
		os.Exit(1)
	}

	// Gather context names from the current config.
	var contextNames []string
	for _, ctx := range origCfg.Contexts {
		contextNames = append(contextNames, ctx.Name)
	}
	// Allow creation of a new context.
	contextNames = append(contextNames, "new context")

	// Ask the user which context to update.
	var selectedContext string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a context to update").
				Options(huh.NewOptions(contextNames...)...).
				Value(&selectedContext),
		),
	).Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error selecting context: %v\n", err)
		os.Exit(1)
	}

	var targetContext *NamedContext
	var newContext bool
	if selectedContext == "new context" {
		newContext = true
		// For a new context, ask for context, cluster, and user names.
		var newCtxName, newClusterName, newUserName string
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Enter new context name").
					Value(&newCtxName),
				huh.NewInput().
					Title("Enter new cluster name").
					Value(&newClusterName),
				huh.NewInput().
					Title("Enter new user name").
					Value(&newUserName),
			),
		).Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting new context details: %v\n", err)
			os.Exit(1)
		}
		newCtx := NamedContext{
			Name: newCtxName,
			Context: Context{
				Cluster: newClusterName,
				User:    newUserName,
			},
		}
		origCfg.Contexts = append(origCfg.Contexts, newCtx)
		targetContext = &origCfg.Contexts[len(origCfg.Contexts)-1]
	} else {
		// Locate the selected context.
		for i, ctx := range origCfg.Contexts {
			if ctx.Name == selectedContext {
				targetContext = &origCfg.Contexts[i]
				break
			}
		}
		if targetContext == nil {
			fmt.Fprintf(os.Stderr, "Context %s not found\n", selectedContext)
			os.Exit(1)
		}
	}

	// For an existing context, ask if the server URL should be updated.
	var updateServer bool
	if !newContext {
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Do you want to update the server URL for cluster %s?", targetContext.Context.Cluster)).
					Value(&updateServer),
			),
		).Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting confirmation for server update: %v\n", err)
			os.Exit(1)
		}
	}

	// Ask the user to paste a kubeconfig file.
	// Set a larger height so that large files are not cut off.
	var pastedKubeconfig string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title("Paste kubeconfig file (press ctrl+d when done)").
				CharLimit(99999).
				Value(&pastedKubeconfig),
		),
	).Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading pasted kubeconfig: %v\n", err)
		os.Exit(1)
	}

	// Parse the pasted kubeconfig.
	var newCfg KubeConfig
	if err := yaml.Unmarshal([]byte(pastedKubeconfig), &newCfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing pasted kubeconfig: %v\n", err)
		os.Exit(1)
	}

	// Determine the target cluster name from the current (or new) context.
	targetClusterName := targetContext.Context.Cluster

	// Look for the target cluster in the pasted kubeconfig.
	var pastedCluster *NamedCluster
	for _, c := range newCfg.Clusters {
		if c.Name == targetClusterName {
			pastedCluster = &c
			break
		}
	}

	// If not found, ask the user to select one from the pasted clusters.
	if pastedCluster == nil {
		var options []string
		for _, c := range newCfg.Clusters {
			options = append(options, c.Name)
		}
		var selectedPastedClusterName string
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("The pasted kubeconfig does not contain a cluster named " + targetClusterName + ". Select a cluster from the pasted file").
					Options(huh.NewOptions(options...)...).
					Value(&selectedPastedClusterName),
			),
		).Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error selecting cluster from pasted kubeconfig: %v\n", err)
			os.Exit(1)
		}
		// Now find the selected cluster.
		for _, c := range newCfg.Clusters {
			if c.Name == selectedPastedClusterName {
				pastedCluster = &c
				// Update the target context cluster name to the selected one.
				targetContext.Context.Cluster = selectedPastedClusterName
				break
			}
		}
		if pastedCluster == nil {
			fmt.Fprintf(os.Stderr, "Error: selected cluster not found in pasted kubeconfig\n")
			os.Exit(1)
		}
	}

	// Find in the pasted kubeconfig a context that uses the selected pasted cluster.
	var pastedContext *NamedContext
	for _, ctx := range newCfg.Contexts {
		if ctx.Context.Cluster == pastedCluster.Name {
			pastedContext = &ctx
			break
		}
	}
	if pastedContext == nil {
		// If no context references the cluster, ask the user to select one.
		var options []string
		for _, ctx := range newCfg.Contexts {
			if ctx.Context.Cluster == pastedCluster.Name {
				options = append(options, ctx.Name)
			}
		}
		if len(options) == 0 {
			fmt.Fprintf(os.Stderr, "Error: no context in pasted kubeconfig references cluster %q\n", pastedCluster.Name)
			os.Exit(1)
		}
		var selectedPastedContextName string
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(fmt.Sprintf("Select a context from pasted kubeconfig for cluster %q", pastedCluster.Name)).
					Options(huh.NewOptions(options...)...).
					Value(&selectedPastedContextName),
			),
		).Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error selecting context from pasted kubeconfig: %v\n", err)
			os.Exit(1)
		}
		for _, ctx := range newCfg.Contexts {
			if ctx.Name == selectedPastedContextName {
				pastedContext = &ctx
				break
			}
		}
	}

	// Using the context from the pasted file, determine the corresponding user.
	targetPastedUserName := pastedContext.Context.User
	var pastedUser *NamedUser
	for _, u := range newCfg.Users {
		if u.Name == targetPastedUserName {
			pastedUser = &u
			break
		}
	}
	if pastedUser == nil {
		// If no matching user is found, ask the user to select one.
		var options []string
		for _, u := range newCfg.Users {
			options = append(options, u.Name)
		}
		var selectedPastedUserName string
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select a user from the pasted kubeconfig").
					Options(huh.NewOptions(options...)...).
					Value(&selectedPastedUserName),
			),
		).Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error selecting user from pasted kubeconfig: %v\n", err)
			os.Exit(1)
		}
		for _, u := range newCfg.Users {
			if u.Name == selectedPastedUserName {
				pastedUser = &u
				break
			}
		}
		if pastedUser == nil {
			fmt.Fprintf(os.Stderr, "Error: selected user not found in pasted kubeconfig\n")
			os.Exit(1)
		}
	}

	// Prepare to record changes.
	var changes []string

	// Update cluster information.
	clusterUpdated := false
	for i, c := range origCfg.Clusters {
		if c.Name == targetContext.Context.Cluster {
			// Compare and update certificate authority data.
			oldCA := c.Cluster.CertificateAuthorityData
			newCA := pastedCluster.Cluster.CertificateAuthorityData
			if oldCA != newCA {
				changes = append(changes, fmt.Sprintf("Updated cluster %q certificateAuthorityData from %s to %s",
					c.Name, shorten(oldCA), shorten(newCA)))
			}
			// Update server if needed.
			if newContext || updateServer {
				oldServer := c.Cluster.Server
				newServer := pastedCluster.Cluster.Server
				if oldServer != newServer {
					changes = append(changes, fmt.Sprintf("Updated cluster %q server from %s to %s",
						c.Name, shorten(oldServer), shorten(newServer)))
				}
				origCfg.Clusters[i].Cluster.Server = newServer
			}
			origCfg.Clusters[i].Cluster.CertificateAuthorityData = newCA
			clusterUpdated = true
			break
		}
	}
	if !clusterUpdated {
		// If the cluster was not present, add it.
		origCfg.Clusters = append(origCfg.Clusters, NamedCluster{
			Name:    targetContext.Context.Cluster,
			Cluster: pastedCluster.Cluster,
		})
		changes = append(changes, fmt.Sprintf("Added new cluster %q with server %s and certificateAuthorityData %s",
			targetContext.Context.Cluster, shorten(pastedCluster.Cluster.Server), shorten(pastedCluster.Cluster.CertificateAuthorityData)))
	}

	// Update user information.
	userUpdated := false
	for i, u := range origCfg.Users {
		if u.Name == targetContext.Context.User {
			oldToken := u.User.Token
			oldCert := u.User.ClientCertificateData
			oldKey := u.User.ClientKeyData

			newToken := pastedUser.User.Token
			newCert := pastedUser.User.ClientCertificateData
			newKey := pastedUser.User.ClientKeyData

			if oldToken != newToken {
				changes = append(changes, fmt.Sprintf("Updated user %q token from %s to %s", u.Name, shorten(oldToken), shorten(newToken)))
			}
			if oldCert != newCert {
				changes = append(changes, fmt.Sprintf("Updated user %q clientCertificateData from %s to %s", u.Name, shorten(oldCert), shorten(newCert)))
			}
			if oldKey != newKey {
				changes = append(changes, fmt.Sprintf("Updated user %q clientKeyData from %s to %s", u.Name, shorten(oldKey), shorten(newKey)))
			}
			origCfg.Users[i].User = pastedUser.User
			userUpdated = true
			break
		}
	}
	if !userUpdated {
		origCfg.Users = append(origCfg.Users, NamedUser{
			Name: targetContext.Context.User,
			User: pastedUser.User,
		})
		changes = append(changes, fmt.Sprintf("Added new user %q with token %s, clientCertificateData %s, and clientKeyData %s",
			targetContext.Context.User, shorten(pastedUser.User.Token), shorten(pastedUser.User.ClientCertificateData), shorten(pastedUser.User.ClientKeyData)))
	}

	// Marshal the updated configuration back to YAML.
	outData, err := yaml.Marshal(&origCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling updated kubeconfig: %v\n", err)
		os.Exit(1)
	}

	// Print the summary of changes.
	fmt.Println("Summary of changes:")
	if len(changes) == 0 {
		fmt.Println("No changes made.")
	} else {
		for _, change := range changes {
			fmt.Println("- " + change)
		}
	}

	// In try mode, simply print the updated configuration.
	if *tryFlag {
		fmt.Println("\n---- Updated kubeconfig (try mode) ----")
		fmt.Println(string(outData))
		return
	}

	// Create a backup of the original file with a .backup.YYYYMMDD extension.
	backupPath := fmt.Sprintf("%s.backup.%s", configPath, time.Now().Format("20060102"))
	if err := ioutil.WriteFile(backupPath, origData, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing backup file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Backup of original kubeconfig saved as %s\n", backupPath)

	// Write the updated configuration back to the file.
	if err := ioutil.WriteFile(configPath, outData, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing updated kubeconfig: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Kubeconfig updated successfully in %s\n", configPath)
}
