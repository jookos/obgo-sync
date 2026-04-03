package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/jookos/obgo/internal/config"
	"github.com/jookos/obgo/internal/couchdb"
	"github.com/jookos/obgo/internal/crypto"
	"github.com/jookos/obgo/internal/sync"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var envFile string

	root := &cobra.Command{
		Use:   "obgo",
		Short: "Sync an Obsidian vault with a CouchDB instance using the Livesync protocol",
	}

	root.PersistentFlags().StringVar(&envFile, "env-file", ".env", "path to .env file")

	root.AddCommand(pullCmd(&envFile))
	root.AddCommand(pushCmd(&envFile))

	return root
}

func pullCmd(envFile *string) *cobra.Command {
	var watch, watchLocal, watchRemote, silence, verbose bool

	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull documents from CouchDB to local vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			wl := watchLocal || watch
			wr := watchRemote || watch
			return run(cmd.Context(), *envFile, wl, wr, silence, verbose, true)
		},
	}
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for both local and remote changes after pull")
	cmd.Flags().BoolVar(&watchLocal, "wl", false, "watch for local changes and push them after pull")
	cmd.Flags().BoolVar(&watchRemote, "wr", false, "watch for remote changes and pull them after pull")
	cmd.Flags().BoolVarP(&silence, "silence", "s", false, "suppress progress output")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "log each file synced during watch")
	return cmd
}

func pushCmd(envFile *string) *cobra.Command {
	var watch, watchLocal, watchRemote, silence, verbose bool

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push local vault files to CouchDB",
		RunE: func(cmd *cobra.Command, args []string) error {
			wl := watchLocal || watch
			wr := watchRemote || watch
			return run(cmd.Context(), *envFile, wl, wr, silence, verbose, false)
		},
	}
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch for both local and remote changes after push")
	cmd.Flags().BoolVar(&watchLocal, "wl", false, "watch for local changes and push them after push")
	cmd.Flags().BoolVar(&watchRemote, "wr", false, "watch for remote changes and pull them after push")
	cmd.Flags().BoolVarP(&silence, "silence", "s", false, "suppress progress output")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "log each file synced during watch")
	return cmd
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func hostFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Host
}

func run(parentCtx context.Context, envFile string, watchLocal, watchRemote, silence, verbose, isPull bool) error {
	// Load .env file if present; ignore error if file is missing.
	_ = godotenv.Load(envFile)

	// Load configuration from environment.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	// Create CouchDB client.
	db, err := couchdb.New(cfg.CouchDBURL)
	if err != nil {
		return fmt.Errorf("CouchDB client error: %w", err)
	}

	// Create crypto service.
	cr := crypto.New(cfg.E2EEPassword)

	// Create sync service.
	svc := sync.New(db, cr, cfg.DataPath)

	// Set up context with cancellation for graceful shutdown.
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// Handle OS signals for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Run the primary operation.
	host := hostFromURL(cfg.CouchDBURL)
	if isPull {
		if !silence {
			fmt.Fprintf(os.Stderr, "Pulling from %s...\n", host)
			var count int
			start := time.Now()
			svc.OnPullFile = func(n int) {
				count = n
				fmt.Fprintf(os.Stderr, "\rPulled %d file(s)", n)
			}
			if err := svc.Pull(ctx); err != nil {
				fmt.Fprintln(os.Stderr)
				return fmt.Errorf("pull failed: %w", err)
			}
			fmt.Fprintf(os.Stderr, "\rPulled %d files in %s\n", count, formatDuration(time.Since(start)))
		} else {
			if err := svc.Pull(ctx); err != nil {
				return fmt.Errorf("pull failed: %w", err)
			}
		}
	} else {
		if !silence {
			fmt.Fprintf(os.Stderr, "Pushing to %s...\n", host)
			var count int
			start := time.Now()
			svc.OnPushFile = func(n int) {
				count = n
				fmt.Fprintf(os.Stderr, "\rPushed %d file(s)", n)
			}
			if err := svc.Push(ctx); err != nil {
				fmt.Fprintln(os.Stderr)
				return fmt.Errorf("push failed: %w", err)
			}
			fmt.Fprintf(os.Stderr, "\rPushed %d files in %s\n", count, formatDuration(time.Since(start)))
		} else {
			if err := svc.Push(ctx); err != nil {
				return fmt.Errorf("push failed: %w", err)
			}
		}
	}

	// Start watch mode if requested.
	if watchLocal || watchRemote {
		if !silence {
			switch {
			case watchLocal && watchRemote:
				fmt.Fprintf(os.Stderr, "Watching for local and remote changes...\n")
			case watchLocal:
				fmt.Fprintf(os.Stderr, "Watching for local changes...\n")
			default:
				fmt.Fprintf(os.Stderr, "Watching for remote changes...\n")
			}
		}
		if verbose {
			svc.OnWatchEvent = func(path string, toRemote bool) {
				if toRemote {
					fmt.Fprintf(os.Stderr, "  → %s (to remote)\n", path)
				} else {
					fmt.Fprintf(os.Stderr, "  ← %s (from remote)\n", path)
				}
			}
		}
		if err := svc.Watch(ctx, watchLocal, watchRemote); err != nil {
			return fmt.Errorf("watch failed: %w", err)
		}
	}

	return nil
}
