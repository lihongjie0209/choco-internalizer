package command

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/lihongjie0209/choco-internalizer/internal/internalize"
	"github.com/lihongjie0209/choco-internalizer/internal/publish"
	"github.com/lihongjie0209/choco-internalizer/internal/syncer"
	"github.com/spf13/cobra"
)

func Execute() int {
	if err := newRootCommand().Execute(); err != nil {
		slog.Error("command failed", "error", err)
		return 1
	}
	return 0
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "choco-internalizer", Short: "Internalize Chocolatey packages without executing package scripts", SilenceErrors: true, SilenceUsage: true}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.AddCommand(newInternalizeCommand())
	root.AddCommand(newSyncCommand())
	return root
}

func newSyncCommand() *cobra.Command {
	var source, repository, apiKeyEnv, output string
	var maxDownloadSize int64
	cmd := &cobra.Command{
		Use: "sync PACKAGE...", Short: "Recursively internalize and publish complete dependency graphs", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, packages []string) error {
			apiKey := os.Getenv(apiKeyEnv)
			if apiKey == "" {
				return fmt.Errorf("environment variable %s is empty", apiKeyEnv)
			}
			service, err := syncer.New(syncer.Options{Source: source, Repository: repository, APIKey: apiKey, Output: output, MaxDownloadSize: maxDownloadSize})
			if err != nil {
				return err
			}
			events, err := service.Sync(cmd.Context(), packages)
			for _, event := range events {
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", event.Status, event.ID, event.Version)
			}
			if err != nil {
				return fmt.Errorf("sync dependency graph: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "https://community.chocolatey.org/api/v2", "upstream NuGet v2 source")
	cmd.Flags().StringVar(&repository, "repository", "https://choco.lihongjie.cn", "destination repository")
	cmd.Flags().StringVar(&apiKeyEnv, "api-key-env", "CHOCO_API_KEY", "environment variable containing the push API key")
	cmd.Flags().StringVarP(&output, "output", "o", "dist", "directory for internalized packages")
	cmd.Flags().Int64Var(&maxDownloadSize, "max-download-size", 4<<30, "maximum bytes for each resource")
	return cmd
}

func newInternalizeCommand() *cobra.Command {
	var input, output, repository, apiKeyEnv string
	var maxDownloadSize int64
	cmd := &cobra.Command{
		Use: "internalize", Short: "Embed remote installers and rewrite the package", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service := internalize.New(&http.Client{Timeout: 2 * time.Hour}, internalize.Options{MaxDownloadSize: maxDownloadSize})
			report, err := service.Run(cmd.Context(), input, output)
			if err != nil {
				return fmt.Errorf("internalize package: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s with %d internalized resource(s)\n", report.Output, len(report.Resources))
			if repository != "" {
				apiKey := os.Getenv(apiKeyEnv)
				if apiKey == "" {
					return fmt.Errorf("environment variable %s is empty", apiKeyEnv)
				}
				skipped, err := publish.New().Push(cmd.Context(), repository, apiKey, report.Output, report.ID, report.Version)
				if err != nil {
					return fmt.Errorf("publish package: %w", err)
				}
				if skipped {
					fmt.Fprintf(cmd.OutOrStdout(), "skipped %s %s: version already exists\n", report.ID, report.Version)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "published %s\n", report.Output)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "input .nupkg path")
	cmd.Flags().StringVarP(&output, "output", "o", "dist", "output directory or .nupkg path")
	cmd.Flags().Int64Var(&maxDownloadSize, "max-download-size", 4<<30, "maximum bytes for each resource")
	cmd.Flags().StringVar(&repository, "repository", "", "repository base URL; publish after validation when set")
	cmd.Flags().StringVar(&apiKeyEnv, "api-key-env", "CHOCO_API_KEY", "environment variable containing the push API key")
	_ = cmd.MarkFlagRequired("input")
	return cmd
}
