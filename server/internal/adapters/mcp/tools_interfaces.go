package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// registerInterfaceTools adds the interface-satisfaction tools to the server.
func registerInterfaceTools(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	addCommit0ResolveInterface(server, deps, log)
}

// ---------------------------------------------------------------------------
// Tool: commit0_resolve_interface
// ---------------------------------------------------------------------------

// resolveInterfaceInput is the typed input for commit0_resolve_interface.
type resolveInterfaceInput struct {
	Qualified    string `json:"qualified"              jsonschema:"Fully qualified name of the Go interface (e.g. 'domain.OpenCodeGraph')."`
	RepoSlug     string `json:"repo_slug"              jsonschema:"Indexed repository slug (e.g. 'commit0-dev/commit0')."`
	WithBindings bool   `json:"with_bindings,omitempty" jsonschema:"If true, attempt to locate constructor call sites (DI wiring) for each implementor. Best-effort. Default false."`
}

func addCommit0ResolveInterface(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_resolve_interface",
		Description: "For a named Go interface, return its required method set and every concrete " +
			"type whose pointer method set satisfies it. Optionally locate the constructor / DI " +
			"wiring sites where each implementor is constructed. Requires a re-index after the " +
			"ImplementsLinker was added — if implementors is empty, trigger a fresh index first.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input resolveInterfaceInput) (*mcpsdk.CallToolResult, any, error) {
		if input.Qualified == "" {
			return toolError(domain.Validation("qualified is required")), nil, nil
		}
		if input.RepoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}

		graph, errResult := graphFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		// Step 1: find the interface node.
		interfaceNode, err := graph.FindNode(ctx, input.RepoSlug, input.Qualified)
		if err != nil {
			var de types.DomainError
			if asDomainError(err, &de) && de.Code == types.ErrNotFound {
				return notFoundResult(input.Qualified), nil, nil
			}
			log.Warn("commit0_resolve_interface find interface failed",
				"qualified", input.Qualified, "err", err)
			return toolError(err), nil, nil
		}

		// Step 2: reverse-traverse on "implements" to find concrete implementors.
		// ImplementsLinker emits edges from=struct, to=interface,
		// so reversing from the interface gives the implementing structs.
		hops, err := graph.TraverseGraph(ctx, interfaceNode.ID,
			[]string{string(types.EdgeImplements)}, "reverse", 1)
		if err != nil {
			log.Warn("commit0_resolve_interface traverse failed",
				"node_id", interfaceNode.ID, "err", err)
			return toolError(err), nil, nil
		}

		implementors := make([]CodeNodeOut, 0, len(hops))
		for _, hop := range hops {
			implementors = append(implementors, codeNodeOut(hop.Node, false))
		}

		// Step 3 (optional): find constructor / DI binding sites per implementor.
		var bindingSites []BindingSiteOut
		if input.WithBindings {
			bindingSites = resolveBindingSites(ctx, graph, hops, log)
		}

		// Build the output methods list from the interface node's Methods field.
		methods := make([]MethodSpecOut, 0, len(interfaceNode.Methods))
		for _, m := range interfaceNode.Methods {
			methods = append(methods, MethodSpecOut{
				Name:      m.Name,
				Signature: m.Signature,
				Receiver:  m.Receiver,
			})
		}

		result := ResolveInterfaceResult{
			Interface:    codeNodeOut(*interfaceNode, false),
			Methods:      methods,
			Implementors: implementors,
			BindingSites: bindingSites,
		}

		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: resolveInterfaceMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}

// resolveBindingSites attempts to find constructor call sites for each implementor.
// This is best-effort: we reverse-traverse on "constructs" from each implementor's
// constructor (if one exists) up to depth 2 to find wiring sites.
func resolveBindingSites(
	ctx context.Context,
	graph domain.OpenCodeGraph,
	hops []types.TraceHop,
	log *slog.Logger,
) []BindingSiteOut {
	var sites []BindingSiteOut
	for _, hop := range hops {
		// Traverse reverse on "constructs" and "calls" from the struct node to
		// find callers that build it. Depth 2 covers New* → wire* → main paths.
		callerHops, err := graph.TraverseGraph(ctx, hop.Node.ID,
			[]string{string(types.EdgeConstructs), string(types.EdgeCalls)},
			"reverse", 2)
		if err != nil {
			log.Warn("commit0_resolve_interface binding traverse failed",
				"node_id", hop.Node.ID, "err", err)
			continue
		}
		for _, ch := range callerHops {
			sites = append(sites, BindingSiteOut{
				ImplementorID:        hop.Node.ID,
				ImplementorQualified: hop.Node.Qualified,
				CallerID:             ch.Node.ID,
				CallerQualified:      ch.Node.Qualified,
				CallerFilePath:       ch.Node.FilePath,
				CallerStartLine:      ch.Node.StartLine,
			})
		}
	}
	return sites
}

// ---------------------------------------------------------------------------
// Output types
// ---------------------------------------------------------------------------

// MethodSpecOut is the MCP-level representation of a MethodSpec.
type MethodSpecOut struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Receiver  string `json:"receiver,omitempty"`
}

// BindingSiteOut represents a single constructor / DI wiring call site.
type BindingSiteOut struct {
	ImplementorID        string `json:"implementor_id"`
	ImplementorQualified string `json:"implementor_qualified"`
	CallerID             string `json:"caller_id"`
	CallerQualified      string `json:"caller_qualified"`
	CallerFilePath       string `json:"caller_file_path"`
	CallerStartLine      int    `json:"caller_start_line,omitempty"`
}

// ResolveInterfaceResult is the structured output of commit0_resolve_interface.
type ResolveInterfaceResult struct {
	Interface    CodeNodeOut      `json:"interface"`
	Methods      []MethodSpecOut  `json:"methods"`
	Implementors []CodeNodeOut    `json:"implementors"`
	BindingSites []BindingSiteOut `json:"binding_sites,omitempty"`
}

// ---------------------------------------------------------------------------
// Markdown formatter
// ---------------------------------------------------------------------------

func resolveInterfaceMarkdown(r ResolveInterfaceResult) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "## Interface `%s`\n\n", r.Interface.Qualified)
	fmt.Fprintf(&sb, "**File:** %s", r.Interface.FilePath)
	if r.Interface.StartLine > 0 {
		fmt.Fprintf(&sb, ":%d", r.Interface.StartLine)
	}
	sb.WriteString("\n\n")

	// Method set
	realMethods := 0
	for _, m := range r.Methods {
		if !strings.HasPrefix(m.Name, "<embedded:") {
			realMethods++
		}
	}
	fmt.Fprintf(&sb, "### Required methods (%d)\n\n", realMethods)
	if len(r.Methods) == 0 {
		sb.WriteString("_No methods found — re-index may be needed._\n\n")
	} else {
		for _, m := range r.Methods {
			if strings.HasPrefix(m.Name, "<embedded:") {
				fmt.Fprintf(&sb, "- _%s_\n", m.Name)
			} else {
				fmt.Fprintf(&sb, "- `%s`\n", m.Signature)
			}
		}
		sb.WriteString("\n")
	}

	// Implementors
	fmt.Fprintf(&sb, "### Implementors (%d)\n\n", len(r.Implementors))
	if len(r.Implementors) == 0 {
		sb.WriteString("_No implementors found. If you expect results, trigger a fresh index " +
			"so the ImplementsLinker can materialize the edges._\n\n")
	} else {
		for i, impl := range r.Implementors {
			fmt.Fprintf(&sb, "%d. `%s` — %s", i+1, impl.Qualified, impl.FilePath)
			if impl.StartLine > 0 {
				fmt.Fprintf(&sb, ":%d", impl.StartLine)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Binding sites (optional)
	if len(r.BindingSites) > 0 {
		fmt.Fprintf(&sb, "### Constructor / DI binding sites (%d, best-effort)\n\n", len(r.BindingSites))
		for _, bs := range r.BindingSites {
			fmt.Fprintf(&sb, "- `%s` ← `%s` (%s",
				bs.ImplementorQualified, bs.CallerQualified, bs.CallerFilePath)
			if bs.CallerStartLine > 0 {
				fmt.Fprintf(&sb, ":%d", bs.CallerStartLine)
			}
			sb.WriteString(")\n")
		}
	}

	return sb.String()
}
