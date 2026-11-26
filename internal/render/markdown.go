package render

import (
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
)

// ToMarkdown converts an HTML fragment into clean Markdown.
func ToMarkdown(fragment string) (string, error) {
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(),
			strikethrough.NewStrikethroughPlugin(),
		),
	)
	md, err := conv.ConvertString(fragment)
	if err != nil {
		return "", err
	}
	return tidyMarkdown(md), nil
}

func tidyMarkdown(md string) string {
	md = strings.ReplaceAll(md, "\u00a0", " ")
	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if strings.HasPrefix(strings.TrimSpace(trimmed), "```") {
			inFence = !inFence
		}
		if !inFence && strings.TrimSpace(trimmed) == "" {
			blank++
			if blank > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, trimmed)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
