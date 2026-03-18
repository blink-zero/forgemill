package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

var (
	cfgFile  string
	jsonOut  bool
	apiURL   string
	apiKey   string
	client   = &http.Client{Timeout: 30 * time.Second}
)

type Config struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
	Token  string `yaml:"token"`
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "forgemill-cli",
		Short: "Forgemill CLI companion tool",
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.forgemill.yaml)")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output as JSON")
	rootCmd.PersistentFlags().StringVar(&apiURL, "url", "", "server URL")
	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "API key")

	rootCmd.AddCommand(loginCmd())
	rootCmd.AddCommand(targetsCmd())
	rootCmd.AddCommand(templatesCmd())
	rootCmd.AddCommand(deployCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(vmsCmd())

	cobra.OnInitialize(initConfig)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func initConfig() {
	cfg := loadConfig()
	if apiURL == "" {
		apiURL = cfg.URL
	}
	if apiKey == "" {
		apiKey = cfg.APIKey
		if apiKey == "" && cfg.Token != "" {
			apiKey = cfg.Token
		}
	}
}

func configPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not determine home directory (%v), using current directory for config\n", err)
		return ".forgemill.yaml"
	}
	return filepath.Join(home, ".forgemill.yaml")
}

func loadConfig() Config {
	var cfg Config
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	yaml.Unmarshal(data, &cfg)
	return cfg
}

func saveConfig(cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}

func doRequest(method, path string, body interface{}) ([]byte, error) {
	if apiURL == "" {
		return nil, fmt.Errorf("server URL not configured, use 'forgemill-cli login --url <url>' or --url flag")
	}

	url := strings.TrimRight(apiURL, "/") + "/api" + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func printResult(data []byte) {
	if jsonOut {
		var buf bytes.Buffer
		json.Indent(&buf, data, "", "  ")
		fmt.Println(buf.String())
		return
	}
	// Default: compact JSON (single line, no extra whitespace)
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, data); err == nil {
		fmt.Println(compacted.String())
	} else {
		fmt.Println(string(data))
	}
}

func printTable(headers []string, rows [][]string) {
	if jsonOut {
		return
	}
	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header
	for i, h := range headers {
		fmt.Printf("%-*s  ", widths[i], h)
	}
	fmt.Println()
	for i := range headers {
		fmt.Printf("%-*s  ", widths[i], strings.Repeat("-", widths[i]))
	}
	fmt.Println()

	// Print rows
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				fmt.Printf("%-*s  ", widths[i], cell)
			}
		}
		fmt.Println()
	}
}

// --- Login ---

func loginCmd() *cobra.Command {
	var serverURL, username string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login and save credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if serverURL == "" {
				return fmt.Errorf("--url is required")
			}
			if username == "" {
				return fmt.Errorf("--user is required")
			}

			// V6-M3: Always prompt interactively with hidden input
			fmt.Print("Password: ")
			pwBytes, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				return fmt.Errorf("error reading password: %w", err)
			}
			fmt.Println() // newline after hidden input
			password := string(pwBytes)

			apiURL = serverURL
			body, err := doRequest("POST", "/auth/login", map[string]string{
				"username": username,
				"password": password,
			})
			if err != nil {
				return fmt.Errorf("login failed: %w", err)
			}

			var result struct {
				Token string `json:"token"`
			}
			if err := json.Unmarshal(body, &result); err != nil {
				return err
			}

			cfg := Config{
				URL:   serverURL,
				Token: result.Token,
			}
			if err := saveConfig(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			fmt.Println("Login successful. Config saved to", configPath())
			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "url", "", "server URL")
	cmd.Flags().StringVar(&username, "user", "", "username")

	return cmd
}

// --- Targets ---

func targetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "targets",
		Short: "Manage targets",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all targets",
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := doRequest("GET", "/targets", nil)
			if err != nil {
				return err
			}
			if jsonOut {
				printResult(body)
				return nil
			}

			var targets []struct {
				ID       int64  `json:"id"`
				Name     string `json:"name"`
				Type     string `json:"type"`
				Hostname string `json:"hostname"`
				Status   string `json:"status"`
			}
			if err := json.Unmarshal(body, &targets); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			rows := [][]string{}
			for _, t := range targets {
				rows = append(rows, []string{
					fmt.Sprintf("%d", t.ID), t.Name, t.Type, t.Hostname, t.Status,
				})
			}
			printTable([]string{"ID", "NAME", "TYPE", "HOSTNAME", "STATUS"}, rows)
			return nil
		},
	}

	cmd.AddCommand(listCmd)
	return cmd
}

// --- Templates ---

func templatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "templates",
		Short: "Manage templates",
	}

	var targetID int64

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/templates"
			if targetID > 0 {
				path = fmt.Sprintf("/templates?target_id=%d", targetID)
			}
			body, err := doRequest("GET", path, nil)
			if err != nil {
				return err
			}
			if jsonOut {
				printResult(body)
				return nil
			}

			var templates []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				OSType     string `json:"os_type"`
				CPU        int    `json:"cpu"`
				MemoryMB   int    `json:"memory_mb"`
				TargetName string `json:"target_name"`
			}
			if err := json.Unmarshal(body, &templates); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			rows := [][]string{}
			for _, t := range templates {
				rows = append(rows, []string{
					fmt.Sprintf("%d", t.ID), t.Name, t.OSType,
					fmt.Sprintf("%d", t.CPU), fmt.Sprintf("%d", t.MemoryMB), t.TargetName,
				})
			}
			printTable([]string{"ID", "NAME", "OS", "CPU", "MEMORY_MB", "TARGET"}, rows)
			return nil
		},
	}

	listCmd.Flags().Int64Var(&targetID, "target-id", 0, "filter by target ID")

	cmd.AddCommand(listCmd)
	return cmd
}

// --- Deploy ---

func deployCmd() *cobra.Command {
	var (
		templateID  int64
		blueprintID int64
		vmName      string
		cpu         int
		memoryMB    int
	)

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			if vmName == "" {
				return fmt.Errorf("--name is required")
			}

			if blueprintID > 0 {
				body, err := doRequest("POST", fmt.Sprintf("/blueprints/%d/deploy", blueprintID), map[string]string{
					"vm_name": vmName,
				})
				if err != nil {
					return err
				}
				printResult(body)
				return nil
			}

			if templateID == 0 {
				return fmt.Errorf("--template or --blueprint is required")
			}

			req := map[string]interface{}{
				"template_id": templateID,
				"vm_name":     vmName,
			}
			if cpu > 0 {
				req["cpu"] = cpu
			}
			if memoryMB > 0 {
				req["memory_mb"] = memoryMB
			}

			body, err := doRequest("POST", "/deploy", req)
			if err != nil {
				return err
			}
			printResult(body)
			return nil
		},
	}

	cmd.Flags().Int64Var(&templateID, "template", 0, "template ID")
	cmd.Flags().Int64Var(&blueprintID, "blueprint", 0, "blueprint ID")
	cmd.Flags().StringVar(&vmName, "name", "", "VM name")
	cmd.Flags().IntVar(&cpu, "cpu", 0, "CPU count")
	cmd.Flags().IntVar(&memoryMB, "memory", 0, "memory in MB")

	return cmd
}

// --- Status ---

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [deployment-id]",
		Short: "Check deployment status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := doRequest("GET", "/deploy/"+args[0], nil)
			if err != nil {
				return err
			}
			if jsonOut {
				printResult(body)
				return nil
			}

			var d struct {
				ID     int64  `json:"id"`
				VMName string `json:"vm_name"`
				Status string `json:"status"`
				Error  string `json:"error_message"`
			}
			if err := json.Unmarshal(body, &d); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			fmt.Printf("Deployment #%d\n", d.ID)
			fmt.Printf("  VM:     %s\n", d.VMName)
			fmt.Printf("  Status: %s\n", d.Status)
			if d.Error != "" {
				fmt.Printf("  Error:  %s\n", d.Error)
			}
			return nil
		},
	}
	return cmd
}

// --- VMs ---

func vmsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vms",
		Short: "Manage VMs",
	}

	// vms list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List managed VMs",
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := doRequest("GET", "/vms", nil)
			if err != nil {
				return err
			}
			if jsonOut {
				printResult(body)
				return nil
			}

			var vms []struct {
				ID         int64  `json:"id"`
				VMName     string `json:"vm_name"`
				PowerState string `json:"power_state"`
				IPAddress  string `json:"ip_address"`
				CPU        int    `json:"cpu"`
				MemoryMB   int    `json:"memory_mb"`
				TargetName string `json:"target_name"`
			}
			if err := json.Unmarshal(body, &vms); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			rows := [][]string{}
			for _, vm := range vms {
				rows = append(rows, []string{
					fmt.Sprintf("%d", vm.ID), vm.VMName, vm.PowerState,
					vm.IPAddress, fmt.Sprintf("%d", vm.CPU),
					fmt.Sprintf("%d", vm.MemoryMB), vm.TargetName,
				})
			}
			printTable([]string{"ID", "NAME", "STATE", "IP", "CPU", "MEMORY_MB", "TARGET"}, rows)
			return nil
		},
	}

	// vms power
	powerCmd := &cobra.Command{
		Use:   "power [vm-id] [start|stop|restart|suspend]",
		Short: "Power operations on a VM",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := args[1]
			if action != "start" && action != "stop" && action != "restart" && action != "suspend" {
				return fmt.Errorf("invalid action: %s (must be start|stop|restart|suspend)", action)
			}
			body, err := doRequest("POST", fmt.Sprintf("/vms/%s/power/%s", args[0], action), nil)
			if err != nil {
				return err
			}
			if jsonOut {
				printResult(body)
				return nil
			}
			fmt.Printf("Power action '%s' executed on VM %s\n", action, args[0])
			return nil
		},
	}

	// vms snapshot
	snapshotCmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Snapshot operations",
	}

	var snapName, snapDesc string
	snapCreateCmd := &cobra.Command{
		Use:   "create [vm-id]",
		Short: "Create a snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if snapName == "" {
				return fmt.Errorf("--name is required")
			}
			body, err := doRequest("POST", fmt.Sprintf("/vms/%s/snapshots", args[0]), map[string]interface{}{
				"name":        snapName,
				"description": snapDesc,
			})
			if err != nil {
				return err
			}
			if jsonOut {
				printResult(body)
				return nil
			}
			fmt.Printf("Snapshot '%s' created on VM %s\n", snapName, args[0])
			return nil
		},
	}
	snapCreateCmd.Flags().StringVar(&snapName, "name", "", "snapshot name")
	snapCreateCmd.Flags().StringVar(&snapDesc, "description", "", "snapshot description")

	snapListCmd := &cobra.Command{
		Use:   "list [vm-id]",
		Short: "List snapshots",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := doRequest("GET", fmt.Sprintf("/vms/%s/snapshots", args[0]), nil)
			if err != nil {
				return err
			}
			printResult(body)
			return nil
		},
	}

	snapshotCmd.AddCommand(snapCreateCmd, snapListCmd)

	// vms delete
	deleteCmd := &cobra.Command{
		Use:   "delete [vm-id]",
		Short: "Delete a VM",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := doRequest("DELETE", fmt.Sprintf("/vms/%s", args[0]), nil)
			if err != nil {
				return err
			}
			fmt.Printf("VM %s deleted\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(listCmd, powerCmd, snapshotCmd, deleteCmd)
	return cmd
}
