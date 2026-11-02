package cli

import (
	"context"
	"errors"
	"strconv"

	"github.com/ContractorKeith/feeder/internal/app"
	"github.com/spf13/cobra"
)

type commandIO struct {
	ctx       context.Context
	d         app.Dependencies
	resolve   func() (string, error)
	overrides func(*cobra.Command) map[string]string
	emit      func(any, []string) error
}

func (c commandIO) collection(cmd *cobra.Command) (app.Collection, error) {
	h, err := c.resolve()
	return app.Collection{Context: c.ctx, Home: h, Dependencies: c.d, Overrides: c.overrides(cmd)}, err
}
func usageArgs(check cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := check(cmd, args); err != nil {
			return app.Invalid(errors.New("invalid arguments; see command --help"))
		}
		return nil
	}
}
func feedCommands(c commandIO) *cobra.Command {
	root := &cobra.Command{Use: "feeds", Short: "Manage subscriptions and retained history", Example: "  feeder feeds list", RunE: func(*cobra.Command, []string) error { return app.Invalid(errors.New("choose a feeds command")) }}
	var tags []string
	var title string
	add := &cobra.Command{Use: "add URL", Short: "Register/reactivate without fetching; first successful fetch establishes baseline", Example: "  feeder feeds add https://example.org/rss --tag tech", Args: usageArgs(cobra.ExactArgs(1))}
	add.Flags().StringArrayVar(&tags, "tag", nil, "subscription tag (repeatable; merged on re-add)")
	add.Flags().StringVar(&title, "title", "", "display title")
	add.RunE = func(cmd *cobra.Command, args []string) error {
		service, err := c.collection(cmd)
		if err != nil {
			return err
		}
		id, err := service.Add(args[0], title, tags)
		if err != nil {
			return err
		}
		return c.emit(map[string]any{"id": id}, nil)
	}
	var all bool
	list := &cobra.Command{Use: "list", Short: "List active subscriptions (--all includes removed history)", Example: "  feeder feeds list --all --json", Args: usageArgs(cobra.NoArgs)}
	list.Flags().BoolVar(&all, "all", false, "include removed subscriptions")
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		service, err := c.collection(cmd)
		if err != nil {
			return err
		}
		feeds, err := service.List(all)
		if err != nil {
			return err
		}
		return c.emit(feeds, nil)
	}
	root.AddCommand(add, list)
	for _, action := range []string{"tag", "remove", "purge"} {
		var replacement []string
		var dry, force bool
		cmd := &cobra.Command{Use: action + " ID", Short: map[string]string{"tag": "Replace subscription tags", "remove": "Stop fetching; retain items and subscription identity", "purge": "Preview/delete a removed subscription and retained items"}[action], Example: "  feeder feeds " + action + " 1", Args: usageArgs(cobra.ExactArgs(1))}
		if action == "tag" {
			cmd.Flags().StringArrayVar(&replacement, "tag", nil, "replacement tag (repeatable; omit to clear)")
		}
		if action == "purge" {
			cmd.Flags().BoolVar(&dry, "dry-run", false, "preview item count")
			cmd.Flags().BoolVar(&force, "force", false, "confirm deletion")
			cmd.Example = "  feeder feeds purge 1 --dry-run"
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id <= 0 {
				return app.Invalid(errors.New("ID must be a positive integer"))
			}
			op := action
			if action == "purge" {
				if dry == force {
					return app.Invalid(errors.New("choose exactly one of --dry-run or --force"))
				}
				if dry {
					op = "preview"
				}
			}
			service, err := c.collection(cmd)
			if err != nil {
				return err
			}
			count, err := service.Change(id, op, replacement)
			if err != nil {
				return err
			}
			return c.emit(map[string]any{"id": id, "operation": op, "items": count}, nil)
		}
		root.AddCommand(cmd)
	}
	return root
}
