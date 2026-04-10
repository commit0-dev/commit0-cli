package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// reportInput is the structured input for the agent's write_report tool.
// Defined locally to avoid importing the server-side agent package.
type reportInput struct {
	Title    string          `json:"title"`
	Summary  string          `json:"summary"`
	Sections []reportSection `json:"sections"`
}

// reportSection is a single section within a report.
type reportSection struct {
	Heading    string   `json:"heading"`
	Content    string   `json:"content,omitempty"`
	Code       string   `json:"code,omitempty"`
	CodeLang   string   `json:"code_lang,omitempty"`
	CallChain  []string `json:"call_chain,omitempty"`
	References []string `json:"references,omitempty"`
}

// renderReport formats a structured agent report for the terminal.
func renderReport(argsJSON string) {
	var report reportInput
	if err := json.Unmarshal([]byte(argsJSON), &report); err != nil {
		fmt.Println(argsJSON)
		return
	}

	w := os.Stdout

	// Title bar
	titleLine := "  " + report.Title + "  "
	bar := strings.Repeat("═", len(titleLine))
	fmt.Fprintf(w, "\n%s\n", bold("╔"+bar+"╗"))
	fmt.Fprintf(w, "%s\n", bold("║"+titleLine+"║"))
	fmt.Fprintf(w, "%s\n", bold("╚"+bar+"╝"))

	// Summary
	if report.Summary != "" {
		fmt.Fprintf(w, "\n%s\n", report.Summary)
	}

	// Sections
	for _, sec := range report.Sections {
		renderSection(w, sec)
	}

	fmt.Fprintln(w)
}

func renderSection(w *os.File, sec reportSection) {
	heading := sec.Heading
	ruler := strings.Repeat("─", max(50-len(heading)-2, 10))
	fmt.Fprintf(w, "\n%s %s\n", cyan("─── "+heading+" "), gray(ruler))

	if sec.Content != "" {
		fmt.Fprintf(w, "\n%s\n", sec.Content)
	}

	if len(sec.CallChain) > 0 {
		fmt.Fprintln(w)
		for i, hop := range sec.CallChain {
			indent := strings.Repeat("  ", i)
			arrow := ""
			if i > 0 {
				arrow = "→ "
			}
			fmt.Fprintf(w, "  %s%s%s\n", gray(indent), cyan(arrow), hop)
		}
	}

	if sec.Code != "" {
		fmt.Fprintln(w)
		for _, line := range strings.Split(sec.Code, "\n") {
			fmt.Fprintf(w, "  %s\n", dim(line))
		}
	}

	if len(sec.References) > 0 {
		fmt.Fprintf(w, "\n  %s %s\n", gray("refs:"), yellow(strings.Join(sec.References, ", ")))
	}
}
