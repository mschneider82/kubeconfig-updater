package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// shorten returns a truncated version of a secret string.
func shorten(s string) string {
	if len(s) <= 15 {
		return s
	}
	return fmt.Sprintf("%s...%s", s[:5], s[len(s)-5:])
}

// shortenBytes base64 encodes the byte slice before shortening.
func shortenBytes(data []byte) string {
	if len(data) == 0 {
		return "<empty>"
	}
	s := base64.StdEncoding.EncodeToString(data)
	if len(s) <= 15 {
		return s
	}
	return fmt.Sprintf("%s...%s", s[:5], s[len(s)-5:])
}

func main() {
	configPathFlag := flag.String("config", "~/.kube/config", "Path to kubeconfig file")
	tryFlag := flag.Bool("try", false, "Try mode: do not update file, just print output")
	flag.Parse()

	// Expand tilde in the config path
	configPath := *configPathFlag
	if strings.HasPrefix(configPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting home directory: %v\n", err)
			os.Exit(1)
		}
		configPath = filepath.Join(home, configPath[1:])
	}

	// Read original kubeconfig content for backup
	origData, err := ioutil.ReadFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading kubeconfig file %s: %v\n", configPath, err)
		os.Exit(1)
	}

	// Parse original kubeconfig
	origCfg, err := clientcmd.Load(origData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing kubeconfig: %v\n", err)
		os.Exit(1)
	}

	// Gather context names
	var contextNames []string
	for name := range origCfg.Contexts {
		contextNames = append(contextNames, name)
	}
	contextNames = append(contextNames, "new context")

	// Select context
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

	var targetContextName string
	var targetContext *api.Context
	var newContext bool

	if selectedContext == "new context" {
		newContext = true
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

		targetContextName = newCtxName // Set the target context name
		origCfg.Contexts[targetContextName] = &api.Context{
			Cluster:  newClusterName,
			AuthInfo: newUserName,
		}
		targetContext = origCfg.Contexts[targetContextName] // Use the target context name
	} else {
		targetContextName = selectedContext                 // Set the target context name
		targetContext = origCfg.Contexts[targetContextName] // Use the target context name
		if targetContext == nil {
			fmt.Fprintf(os.Stderr, "Context %s not found\n", selectedContext)
			os.Exit(1)
		}
	}

	var updateServer bool
	if !newContext {
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Update server URL for cluster %s?", targetContext.Cluster)).
					Value(&updateServer),
			),
		).Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting server update confirmation: %v\n", err)
			os.Exit(1)
		}
	}

	// Get pasted kubeconfig
	var pastedKubeconfig string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title("Paste kubeconfig (ctrl+d when done)").
				CharLimit(99999).
				Value(&pastedKubeconfig),
		),
	).Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading pasted kubeconfig: %v\n", err)
		os.Exit(1)
	}

	newCfg, err := clientcmd.Load([]byte(pastedKubeconfig))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing pasted kubeconfig: %v\n", err)
		os.Exit(1)
	}

	targetClusterName := targetContext.Cluster
	pastedCluster, exists := newCfg.Clusters[targetClusterName]
	if !exists {
		var clusterOptions []string
		for name := range newCfg.Clusters {
			clusterOptions = append(clusterOptions, name)
		}
		var selectedCluster string
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select cluster from pasted config").
					Options(huh.NewOptions(clusterOptions...)...).
					Value(&selectedCluster),
			),
		).Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error selecting cluster: %v\n", err)
			os.Exit(1)
		}
		pastedCluster = newCfg.Clusters[selectedCluster]
		targetContext.Cluster = selectedCluster
		targetClusterName = selectedCluster
	}

	var pastedContextName string
	for name, ctx := range newCfg.Contexts {
		if ctx.Cluster == targetClusterName {
			pastedContextName = name
			break
		}
	}
	if pastedContextName == "" {
		var ctxOptions []string
		for name, ctx := range newCfg.Contexts {
			if ctx.Cluster == targetClusterName {
				ctxOptions = append(ctxOptions, name)
			}
		}
		if len(ctxOptions) == 0 {
			fmt.Fprintf(os.Stderr, "No contexts for cluster %s in pasted config\n", targetClusterName)
			os.Exit(1)
		}
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select context from pasted config").
					Options(huh.NewOptions(ctxOptions...)...).
					Value(&pastedContextName),
			),
		).Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error selecting context: %v\n", err)
			os.Exit(1)
		}
	}
	pastedContext := newCfg.Contexts[pastedContextName]

	pastedUser, exists := newCfg.AuthInfos[pastedContext.AuthInfo]
	if !exists {
		var userOptions []string
		for name := range newCfg.AuthInfos {
			userOptions = append(userOptions, name)
		}
		var selectedUser string
		err = huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select user from pasted config").
					Options(huh.NewOptions(userOptions...)...).
					Value(&selectedUser),
			),
		).Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error selecting user: %v\n", err)
			os.Exit(1)
		}
		pastedUser = newCfg.AuthInfos[selectedUser]
	}

	var changes []string

	// Update cluster
	existingCluster, exists := origCfg.Clusters[targetClusterName]
	if exists {
		if (updateServer || newContext) && existingCluster.Server != pastedCluster.Server {
			changes = append(changes, fmt.Sprintf("Updated cluster %q server from %s to %s",
				targetClusterName, existingCluster.Server, pastedCluster.Server))
			existingCluster.Server = pastedCluster.Server
		}
		if !bytes.Equal(existingCluster.CertificateAuthorityData, pastedCluster.CertificateAuthorityData) {
			changes = append(changes, fmt.Sprintf("Updated cluster %q CA data from %s to %s",
				targetClusterName, shortenBytes(existingCluster.CertificateAuthorityData), shortenBytes(pastedCluster.CertificateAuthorityData)))
			existingCluster.CertificateAuthorityData = pastedCluster.CertificateAuthorityData
		}
	} else {
		origCfg.Clusters[targetClusterName] = pastedCluster
		changes = append(changes, fmt.Sprintf("Added cluster %q with server %s and CA data %s",
			targetClusterName, pastedCluster.Server, shortenBytes(pastedCluster.CertificateAuthorityData)))
	}

	// Update user
	targetUserName := targetContext.AuthInfo
	existingUser, exists := origCfg.AuthInfos[targetUserName]
	if exists {
		if existingUser.Token != pastedUser.Token {
			changes = append(changes, fmt.Sprintf("Updated user %q token from %s to %s",
				targetUserName, shorten(existingUser.Token), shorten(pastedUser.Token)))
			existingUser.Token = pastedUser.Token
		}
		if !bytes.Equal(existingUser.ClientCertificateData, pastedUser.ClientCertificateData) {
			changes = append(changes, fmt.Sprintf("Updated user %q client cert from %s to %s",
				targetUserName, shortenBytes(existingUser.ClientCertificateData), shortenBytes(pastedUser.ClientCertificateData)))
			existingUser.ClientCertificateData = pastedUser.ClientCertificateData
		}
		if !bytes.Equal(existingUser.ClientKeyData, pastedUser.ClientKeyData) {
			changes = append(changes, fmt.Sprintf("Updated user %q client key from %s to %s",
				targetUserName, shortenBytes(existingUser.ClientKeyData), shortenBytes(pastedUser.ClientKeyData)))
			existingUser.ClientKeyData = pastedUser.ClientKeyData
		}
	} else {
		origCfg.AuthInfos[targetUserName] = pastedUser
		changes = append(changes, fmt.Sprintf("Added user %q with token %s, client cert %s, and client key %s",
			targetUserName, shorten(pastedUser.Token), shortenBytes(pastedUser.ClientCertificateData), shortenBytes(pastedUser.ClientKeyData)))
	}

	// Print changes
	fmt.Println("Summary of changes:")
	if len(changes) == 0 {
		fmt.Println("No changes made.")
	} else {
		for _, change := range changes {
			fmt.Println("- " + change)
		}
	}

	// Handle try mode
	if *tryFlag {
		outData, err := clientcmd.Write(*origCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling config: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("\n---- Updated kubeconfig (try mode) ----")
		fmt.Println(string(outData))
		return
	}

	// Create backup
	backupPath := fmt.Sprintf("%s.backup.%s", configPath, time.Now().Format(time.RFC3339))
	if err := ioutil.WriteFile(backupPath, origData, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating backup: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Backup saved to %s\n", backupPath)

	// Write updated config
	outData, err := clientcmd.Write(*origCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling updated config: %v\n", err)
		os.Exit(1)
	}
	if err := ioutil.WriteFile(configPath, outData, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing updated config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Successfully updated %s\n", configPath)
}
