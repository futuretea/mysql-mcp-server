package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"example.invalid/mcp-template-module-placeholder/internal/toolcatalog"
	"example.invalid/mcp-template-module-placeholder/pkg/core/config"
	"example.invalid/mcp-template-module-placeholder/pkg/core/version"
	"example.invalid/mcp-template-module-placeholder/pkg/toolset"
	"example.invalid/mcp-template-module-placeholder/pkg/toolset/example"
)

// IOStreams groups the streams used by the CLI.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

// NewRootCommand creates the CLI root. The MCP server is started via the `mcp` subcommand.
func NewRootCommand(streams IOStreams) *cobra.Command {
	var cfgFile string
	v := viper.New()

	command := &cobra.Command{
		Use:           version.BinaryName,
		Short:         "MCP server template CLI and Model Context Protocol server",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	command.SetIn(streams.In)
	command.SetOut(streams.Out)
	command.SetErr(streams.ErrOut)

	addCommonFlags(command, &cfgFile)
	command.AddCommand(newMCPCommand(streams, &cfgFile, v))
	command.AddCommand(newToolsCommand(streams, &cfgFile, v))
	command.AddCommand(newVersionCommand(streams))
	command.AddCommand(newCompletionCommand(streams))
	return command
}

func addCommonFlags(command *cobra.Command, cfgFile *string) {
	flags := command.PersistentFlags()
	flags.StringVar(cfgFile, "config", "", "config file path (YAML)")
	flags.StringSlice("enabled-tools", []string{}, "comma-separated list of tool names to enable")
	flags.StringSlice("disabled-tools", []string{}, "comma-separated list of tool names to disable")
	flags.StringSlice("enable-domains", []string{}, "comma-separated list of tool domains to enable")
	flags.StringSlice("disable-domains", []string{}, "comma-separated list of tool domains to disable")
}

func bindCommonFlags(v *viper.Viper, cmd *cobra.Command) error {
	flags := cmd.Root().PersistentFlags()
	bindings := map[string]string{
		"enabled_tools":    "enabled-tools",
		"disabled_tools":   "disabled-tools",
		"enabled_domains":  "enable-domains",
		"disabled_domains": "disable-domains",
	}
	for key, flag := range bindings {
		if err := v.BindPFlag(key, flags.Lookup(flag)); err != nil {
			return fmt.Errorf("bind flag %s: %w", flag, err)
		}
	}
	return nil
}

func loadCLIConfig(cmd *cobra.Command, cfgFile string, v *viper.Viper) (*config.StaticConfig, error) {
	if err := bindCommonFlags(v, cmd); err != nil {
		return nil, err
	}
	cfg, err := config.LoadConfig(cfgFile, v)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func defaultToolsets() []toolset.Toolset {
	return []toolset.Toolset{&example.Toolset{}}
}

func buildToolCatalog(cfg *config.StaticConfig) ([]toolset.ServerTool, error) {
	return toolcatalog.Build(defaultToolsets(), toolset.FilterOptions{
		EnabledTools:    cfg.EnabledTools,
		DisabledTools:   cfg.DisabledTools,
		EnabledDomains:  cfg.EnabledDomains,
		DisabledDomains: cfg.DisabledDomains,
	})
}

func newVersionCommand(streams IOStreams) *cobra.Command {
	command := &cobra.Command{
		Use:   "version",
		Short: "print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(streams.Out, version.GetVersionInfo())
			return err
		},
	}
	command.SetOut(streams.Out)
	command.SetErr(streams.ErrOut)
	return command
}
