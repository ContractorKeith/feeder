package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// degraded is emitted already; Execute only returns its exit status.
type degraded struct{}

func (degraded) Error() string { return "partial feed failure" }
func fetchCommand(c commandIO, asJSON *bool) *cobra.Command {
	return &cobra.Command{Use: "fetch", Short: "Collect active RSS/Atom feeds; initial successful fetch stores baseline", Example: "  feeder fetch --json", Args: usageArgs(cobra.NoArgs), RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := c.collection(cmd)
		if err != nil {
			return err
		}
		results, err := service.Fetch()
		if err != nil {
			return err
		}
		problems := []problem{}
		for _, r := range results {
			if r.Error != "" {
				problems = append(problems, problem{Code: "feed_fetch", Message: r.Error, FeedID: r.FeedID})
			}
		}
		if len(problems) == 0 {
			return c.emit(results, nil)
		}
		if *asJSON {
			if err = json.NewEncoder(cmd.OutOrStdout()).Encode(envelope{1, "degraded", results, []string{}, problems}); err != nil {
				return err
			}
		} else {
			if err = c.emit(results, nil); err != nil {
				return err
			}
			for _, p := range problems {
				if _, err = fmt.Fprintf(cmd.ErrOrStderr(), "feed %d: %s\n", p.FeedID, p.Message); err != nil {
					return err
				}
			}
		}
		return degraded{}
	}}
}
