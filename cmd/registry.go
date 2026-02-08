package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage the API registry — paid endpoints AgentPay knows about",
}

var registryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List known paid APIs",
	RunE:  runRegistryList,
}

var registryAddCmd = &cobra.Command{
	Use:   "add <name> <url> <protocol>",
	Short: "Add a paid API to the registry",
	Args:  cobra.ExactArgs(3),
	RunE:  runRegistryAdd,
}

func init() {
	registryCmd.AddCommand(registryListCmd)
	registryCmd.AddCommand(registryAddCmd)
}

// APIEntry represents a known paid API.
type APIEntry struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Protocol    string `json:"protocol"` // "x402", "l402", "auto"
	Description string `json:"description,omitempty"`
	CostHint    string `json:"cost_hint,omitempty"`
}

func registryPath() string {
	home, _ := os.UserHomeDir()
	return fmt.Sprintf("%s/.agentpay/registry.json", home)
}

func loadRegistry() ([]APIEntry, error) {
	data, err := os.ReadFile(registryPath())
	if os.IsNotExist(err) {
		return defaultRegistry(), nil
	}
	if err != nil {
		return nil, err
	}
	var entries []APIEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func saveRegistry(entries []APIEntry) error {
	data, _ := json.MarshalIndent(entries, "", "  ")
	return os.WriteFile(registryPath(), data, 0600)
}

func defaultRegistry() []APIEntry {
	return []APIEntry{
		{
			Name:        "maximumsats-dvm",
			URL:         "https://maximumsats.joel-dfd.workers.dev/api/dvm",
			Protocol:    "l402",
			Description: "AI text generation via Nostr DVM",
			CostHint:    "10 sats",
		},
		{
			Name:        "opspawn-a2a",
			URL:         "https://a2a.opspawn.com",
			Protocol:    "x402",
			Description: "OpSpawn A2A gateway for agent-to-agent tasks",
			CostHint:    "$0.01 USDC",
		},
	}
}

func runRegistryList(cmd *cobra.Command, args []string) error {
	entries, err := loadRegistry()
	if err != nil {
		return err
	}

	fmt.Println("Known Paid APIs")
	fmt.Println("===============")
	for _, e := range entries {
		fmt.Printf("\n  %s (%s)\n", e.Name, e.Protocol)
		fmt.Printf("    URL:  %s\n", e.URL)
		if e.Description != "" {
			fmt.Printf("    Desc: %s\n", e.Description)
		}
		if e.CostHint != "" {
			fmt.Printf("    Cost: %s\n", e.CostHint)
		}
	}
	return nil
}

func runRegistryAdd(cmd *cobra.Command, args []string) error {
	entries, err := loadRegistry()
	if err != nil {
		return err
	}

	entry := APIEntry{
		Name:     args[0],
		URL:      args[1],
		Protocol: args[2],
	}
	entries = append(entries, entry)

	if err := saveRegistry(entries); err != nil {
		return err
	}

	fmt.Printf("Added %s (%s) → %s\n", entry.Name, entry.Protocol, entry.URL)
	return nil
}
