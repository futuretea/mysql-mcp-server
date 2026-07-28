package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"example.invalid/mcp-template-module-placeholder/pkg/toolset"
)

func newToolsCommand(streams IOStreams, cfgFile *string, v *viper.Viper) *cobra.Command {
	command := &cobra.Command{
		Use:     "tools",
		Aliases: []string{"tool"},
		Short:   "list, describe, and call MCP tools",
	}
	command.AddCommand(newToolsListCommand(streams, cfgFile, v))
	command.AddCommand(newToolsDescribeCommand(streams, cfgFile, v))
	command.AddCommand(newToolsCallCommand(streams, cfgFile, v))
	return command
}

func newToolsListCommand(streams IOStreams, cfgFile *string, v *viper.Viper) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "list enabled tools",
		Example: `  mcp-template-binary-placeholder tools list
  mcp-template-binary-placeholder tools list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCLIConfig(cmd, *cfgFile, v)
			if err != nil {
				return err
			}
			tools, err := buildToolCatalog(cfg)
			if err != nil {
				return err
			}
			if jsonOutput {
				return printToolSummariesJSON(streams.Out, tools)
			}
			return printToolSummariesTable(streams.Out, tools)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print tools as JSON")
	return command
}

func newToolsDescribeCommand(streams IOStreams, cfgFile *string, v *viper.Viper) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:     "describe <tool-name>",
		Aliases: []string{"schema"},
		Short:   "describe an enabled tool schema",
		Example: `  mcp-template-binary-placeholder tools describe echo
  mcp-template-binary-placeholder tools describe echo --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCLIConfig(cmd, *cfgFile, v)
			if err != nil {
				return err
			}
			tools, err := buildToolCatalog(cfg)
			if err != nil {
				return err
			}
			tool, ok := findTool(tools, args[0])
			if !ok {
				return fmt.Errorf("unknown tool %q; run 'mcp-template-binary-placeholder tools list' to see available tools", args[0])
			}
			description := newToolDescription(tool)
			if jsonOutput {
				enc := json.NewEncoder(streams.Out)
				enc.SetIndent("", "  ")
				return enc.Encode(description)
			}
			return printToolDescription(streams.Out, description)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "print tool schema as JSON")
	return command
}

func newToolsCallCommand(streams IOStreams, cfgFile *string, v *viper.Viper) *cobra.Command {
	var rawParams string
	var paramsFile string
	command := &cobra.Command{
		Use:   "call <tool-name>",
		Short: "call an enabled tool with JSON parameters",
		Example: `  mcp-template-binary-placeholder tools call echo --params '{"message":"hello"}'
  echo '{"message":"hello"}' | mcp-template-binary-placeholder tools call echo --params-file -
  mcp-template-binary-placeholder tools call ping --params '{}'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCLIConfig(cmd, *cfgFile, v)
			if err != nil {
				return err
			}
			params, err := parseToolParams(rawParams, paramsFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			tools, err := buildToolCatalog(cfg)
			if err != nil {
				return err
			}
			tool, ok := findTool(tools, args[0])
			if !ok {
				return fmt.Errorf("unknown tool %q; run 'mcp-template-binary-placeholder tools list' to see available tools", args[0])
			}
			result, err := tool.Handler(cmd.Context(), params)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(streams.Out, result)
			return err
		},
	}
	command.Flags().StringVar(&rawParams, "params", "{}", "tool parameters as a JSON object")
	command.Flags().StringVar(&paramsFile, "params-file", "", "path to a JSON file containing tool parameters; use - to read stdin")
	return command
}

func findTool(tools []toolset.ServerTool, name string) (toolset.ServerTool, bool) {
	for _, tool := range tools {
		if tool.Tool.Name == name {
			return tool, true
		}
	}
	return toolset.ServerTool{}, false
}

type toolSummary struct {
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	Description string `json:"description,omitempty"`
}

type toolDescription struct {
	Name        string         `json:"name"`
	Domain      string         `json:"domain"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

func newToolDescription(tool toolset.ServerTool) toolDescription {
	desc := toolDescription{
		Name:        tool.Tool.Name,
		Domain:      tool.Domain,
		Description: tool.Tool.Description,
	}
	if tool.Tool.RawInputSchema != nil {
		_ = json.Unmarshal(tool.Tool.RawInputSchema, &desc.InputSchema)
	} else if tool.Tool.InputSchema.Type != "" || len(tool.Tool.InputSchema.Properties) > 0 {
		raw, err := json.Marshal(tool.Tool.InputSchema)
		if err == nil {
			_ = json.Unmarshal(raw, &desc.InputSchema)
		}
	}
	return desc
}

func printToolSummariesTable(out io.Writer, tools []toolset.ServerTool) error {
	sorted := append([]toolset.ServerTool(nil), tools...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Tool.Name < sorted[j].Tool.Name
	})
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tDOMAIN\tDESCRIPTION")
	for _, tool := range sorted {
		desc := strings.ReplaceAll(tool.Tool.Description, "\n", " ")
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", tool.Tool.Name, tool.Domain, desc)
	}
	return w.Flush()
}

func printToolSummariesJSON(out io.Writer, tools []toolset.ServerTool) error {
	summaries := make([]toolSummary, 0, len(tools))
	for _, tool := range tools {
		summaries = append(summaries, toolSummary{
			Name:        tool.Tool.Name,
			Domain:      tool.Domain,
			Description: tool.Tool.Description,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(summaries)
}

func printToolDescription(out io.Writer, description toolDescription) error {
	if _, err := fmt.Fprintf(out, "Name:        %s\n", description.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Domain:      %s\n", description.Domain); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Description: %s\n", description.Description); err != nil {
		return err
	}
	if description.InputSchema != nil {
		raw, err := json.MarshalIndent(description.InputSchema, "", "  ")
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "InputSchema:\n%s\n", raw); err != nil {
			return err
		}
	}
	return nil
}

func parseToolParams(rawParams, paramsFile string, stdin io.Reader) (map[string]any, error) {
	payload := strings.TrimSpace(rawParams)
	if paramsFile != "" {
		var data []byte
		var err error
		if paramsFile == "-" {
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(paramsFile)
		}
		if err != nil {
			return nil, fmt.Errorf("read params file: %w", err)
		}
		payload = string(data)
	}
	if strings.TrimSpace(payload) == "" {
		payload = "{}"
	}
	params := map[string]any{}
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		return nil, fmt.Errorf("parse tool params JSON: %w", err)
	}
	if params == nil {
		return nil, fmt.Errorf("parse tool params JSON: expected an object")
	}
	return params, nil
}
