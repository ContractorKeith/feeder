package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/ContractorKeith/feeder/internal/app"
	"github.com/ContractorKeith/feeder/internal/output"
	"github.com/spf13/cobra"
)

func opmlCommands(c commandIO) []*cobra.Command {
	imp := &cobra.Command{Use: "import FILE_OR_DASH", Short: "Import nested OPML transactionally without fetching (5 MiB maximum)", Example: "  feeder import feeds.opml", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		var reader io.Reader = c.d.Stdin
		if args[0] != "-" {
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			reader = f
		}
		service, err := c.collection(cmd)
		if err != nil {
			return err
		}
		ids, err := service.Import(reader)
		if err != nil {
			return err
		}
		return c.emit(map[string]any{"feed_ids": ids}, nil)
	}}
	var path string
	var force bool
	exp := &cobra.Command{Use: "export", Short: "Export active subscriptions as OPML; categories are comma-separated tags", Example: "  feeder export --output feeds.opml", Args: usageArgs(cobra.NoArgs)}
	exp.Flags().StringVar(&path, "output", "-", "output file or - for stdout (atomic, no overwrite)")
	exp.Flags().BoolVar(&force, "force", false, "overwrite an existing regular output file")
	exp.RunE = func(cmd *cobra.Command, _ []string) error {
		h, err := c.resolve()
		if err != nil {
			return err
		}
		if err = output.Validate(h, path, force); err != nil {
			return app.Invalid(err)
		}
		service, err := c.collection(cmd)
		if err != nil {
			return err
		}
		b, err := service.Export()
		if err != nil {
			return err
		}
		if path == "-" {
			_, err = fmt.Fprint(cmd.OutOrStdout(), string(b))
			return err
		}
		if err = output.Write(h, path, force, b); err != nil {
			return err
		}
		return c.emit(map[string]any{"output": path}, nil)
	}
	return []*cobra.Command{imp, exp}
}
