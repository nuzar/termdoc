package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nuzar/termdoc/internal/model"
)

func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "search <query>",
		Aliases: []string{"s", "find"},
		Short:   "Search installed docsets",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			docset, _ := cmd.Flags().GetString("docset")
			limit, _ := cmd.Flags().GetInt("limit")
			asJSON, _ := cmd.Flags().GetBool("json")
			includeAux, _ := cmd.Flags().GetBool("all")
			return runSearchQuery(strings.Join(args, " "), docset, limit, asJSON, includeAux)
		},
	}
	cmd.Flags().StringP("docset", "d", "", "limit search to one docset")
	cmd.Flags().IntP("limit", "n", 20, "maximum number of results (0 = all)")
	cmd.Flags().Bool("json", false, "output JSON")
	cmd.Flags().Bool("all", false, "include samples, examples and guides")
	return cmd
}

func runSearch(cmd *cobra.Command, query string) error {
	return runSearchQuery(query, "", 20, false, false)
}

func runSearchQuery(query, docset string, limit int, asJSON, includeAux bool) error {
	results, err := searchDocs(docset, query, limit, includeAux)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("no matches for %q", query)
	}

	if asJSON {
		type row struct {
			Docset string `json:"docset"`
			Title  string `json:"title"`
			Name   string `json:"name"`
			Type   string `json:"type,omitempty"`
			Score  int    `json:"score"`
		}
		rows := make([]row, 0, len(results))
		for _, r := range results {
			rows = append(rows, row{Docset: r.DocsetID, Title: r.DocsetTitle, Name: r.Entry.Name, Type: r.Entry.Type, Score: r.Score})
		}
		enc := json.NewEncoder(app.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	nameW, typeW := 4, 4
	for _, r := range results {
		if n := len(r.Entry.Name); n > nameW {
			nameW = n
		}
		if n := len(r.Entry.Type); n > typeW {
			typeW = n
		}
	}
	if nameW > 52 {
		nameW = 52
	}
	if typeW > 16 {
		typeW = 16
	}
	for _, r := range results {
		name := pad(truncate(r.Entry.Name, nameW), nameW)
		typ := pad(truncate(r.Entry.Type, typeW), typeW)
		line := "  " + app.cyan(name) + "  " + app.dim(typ) + "  " + app.yellow(r.DocsetID)
		app.printf("%s\n", strings.TrimRight(line, " "))
	}
	if app.Color {
		app.printf("\n%s\n", app.dim(fmt.Sprintf("Run `termdoc show <name>` to read an entry (%d result(s)).", len(results))))
	}
	return nil
}

func searchDocs(docsel, query string, limit int, includeAux bool) ([]model.Result, error) {
	metas, err := app.Store.Resolve(docsel)
	if err != nil {
		return nil, err
	}
	if len(metas) == 0 {
		return nil, fmt.Errorf("no docsets installed\nHint: run `termdoc docs install go` or `termdoc docs import <file>`")
	}
	var out []model.Result
	for _, m := range metas {
		d, err := app.Store.Load(m.ID)
		if err != nil {
			app.eprintf("warning: %v\n", err)
			continue
		}
		for _, h := range d.Search(query, 0) {
			out = append(out, model.Result{
				DocsetID:    m.ID,
				DocsetTitle: m.Title,
				Entry:       d.Entries[h.Index],
				Score:       h.Score,
			})
		}
		d.Close()
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		ni, nj := len(out[i].Entry.Name), len(out[j].Entry.Name)
		if ni != nj {
			return ni < nj
		}
		if out[i].Entry.Name != out[j].Entry.Name {
			return out[i].Entry.Name < out[j].Entry.Name
		}
		return out[i].DocsetID < out[j].DocsetID
	})
	if !includeAux {
		var primary []model.Result
		for _, r := range out {
			if typeRank(r.Entry.Type) < 9 {
				primary = append(primary, r)
			}
		}
		if len(primary) > 0 {
			out = primary
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
