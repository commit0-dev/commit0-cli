package linkers

import (
	"strings"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ImplementsLinker emits EdgeImplements edges for each (concrete type, interface)
// pair where the concrete type's pointer method set covers the interface's required
// method set.
//
// Algorithm:
//  1. Walk sym.Nodes for NodeClass entries that have a non-empty Methods slice.
//  2. Classify each class as an interface (all methods have Receiver=="") or a
//     struct/concrete type (at least one method has a non-empty Receiver).
//  3. For each (struct, interface) pair: the struct's POINTER method set covers the
//     interface iff every interface method has a matching struct method with the same
//     Name and Signature (after stripping generic type params).
//     Go rule: a *T method set includes both T and *T methods.
//  4. Emit types.CodeEdge{Kind: EdgeImplements, FromID: struct.ID, ToID: interface.ID}
//     for each satisfied pair.
//
// Stats: Processed = number of (interface node)s considered;
// Resolved = number of (struct, interface) edges emitted.
type ImplementsLinker struct{}

// Name returns the linker identifier.
func (l *ImplementsLinker) Name() string { return "implements" }

// Labels returns the edge kinds this linker produces.
func (l *ImplementsLinker) Labels() []types.EdgeKind {
	return []types.EdgeKind{types.EdgeImplements}
}

// Link examines the symbol table for class nodes with method sets and emits
// EdgeImplements for every (concrete type, interface) pair where the concrete
// type satisfies the interface. The incoming edges slice is passed through
// unchanged; new implements edges are appended and the combined slice returned.
func (l *ImplementsLinker) Link(edges []types.CodeEdge, sym *domain.SymbolTable) ([]types.CodeEdge, domain.LinkStats) {
	stats := domain.LinkStats{LinkerName: l.Name()}

	// --- Phase A: bucket class nodes into interfaces vs concrete types ---
	type classInfo struct {
		id        string
		qualified string
		// methodSet is the POINTER method set for structs (includes both T and *T
		// methods). For interfaces it's the required method set.
		methodSet   map[string]string // Name → Signature (normalized)
		isInterface bool
	}

	var interfaces []*classInfo
	var structs []*classInfo

	for id, meta := range sym.Nodes {
		if meta.Kind != types.NodeClass {
			continue
		}
		if len(meta.Methods) == 0 {
			continue
		}

		// Determine: interface if every method has Receiver==""; struct otherwise.
		isIface := true
		for _, m := range meta.Methods {
			if m.Receiver != "" {
				isIface = false
				break
			}
		}

		info := &classInfo{
			id:          id,
			qualified:   meta.Qualified,
			methodSet:   buildMethodSet(meta.Methods, isIface),
			isInterface: isIface,
		}
		if isIface {
			interfaces = append(interfaces, info)
		} else {
			structs = append(structs, info)
		}
	}

	// --- Phase B: match structs against interfaces ---
	stats.Processed = len(interfaces)

	for _, iface := range interfaces {
		// Skip empty interfaces — every type satisfies them, which is noise.
		if len(iface.methodSet) == 0 {
			continue
		}
		// Skip interfaces that only have embedded placeholders (<embedded:X>).
		if allEmbedded(iface.methodSet) {
			continue
		}
		for _, concrete := range structs {
			if concrete.id == iface.id {
				continue
			}
			if coversInterface(concrete.methodSet, iface.methodSet) {
				edges = append(edges, types.CodeEdge{
					Kind:   types.EdgeImplements,
					FromID: concrete.id,
					ToID:   iface.id,
				})
				stats.Resolved++
			}
		}
	}

	return edges, stats
}

// buildMethodSet returns a Name→NormalisedSignature map for a method set.
// For struct (non-interface) nodes the pointer method set is used: all methods
// regardless of whether they have T or *T receivers are included (because a
// *T value can call both T and *T methods in Go).
// Embedded interface placeholders (<embedded:X>) are included as-is so that
// allEmbedded can detect them; they are excluded from coverage checks by coversInterface.
func buildMethodSet(methods []types.MethodSpec, isInterface bool) map[string]string {
	set := make(map[string]string, len(methods))
	for _, m := range methods {
		if m.Name == "" {
			continue
		}
		if isInterface {
			// Interface methods: store as-is (Receiver is always "").
			set[m.Name] = normaliseSignature(m.Signature)
		} else {
			// Struct pointer method set: include all methods regardless of
			// whether the receiver is T or *T.
			set[m.Name] = normaliseSignature(m.Signature)
		}
	}
	return set
}

// coversInterface returns true if concreteSet contains an entry for every
// required method in ifaceSet, with matching normalized signatures.
// Embedded-interface placeholder entries (name starts with "<embedded:") in
// ifaceSet are skipped — they are not real method requirements from the
// extractor's point of view.
func coversInterface(concreteSet, ifaceSet map[string]string) bool {
	for name, ifaceSig := range ifaceSet {
		if strings.HasPrefix(name, "<embedded:") {
			continue // embedded placeholder — skip
		}
		concreteSig, ok := concreteSet[name]
		if !ok {
			return false
		}
		// Compare signatures after normalisation. If the interface method has
		// a signature, the concrete method's signature must match.
		if ifaceSig != "" && concreteSig != ifaceSig {
			return false
		}
	}
	return true
}

// allEmbedded returns true if every key in the method set is an embedded
// interface placeholder — in that case the "real" method set is unknown and
// we skip the coverage check.
func allEmbedded(methodSet map[string]string) bool {
	if len(methodSet) == 0 {
		return false
	}
	for name := range methodSet {
		if !strings.HasPrefix(name, "<embedded:") {
			return false
		}
	}
	return true
}

// normaliseSignature strips generic type parameters and trims whitespace from
// a method signature before comparison. This matches the extractor's
// stripGoGenericParams behavior.
func normaliseSignature(sig string) string {
	if !strings.Contains(sig, "[") {
		return strings.TrimSpace(sig)
	}
	var result strings.Builder
	depth := 0
	for _, ch := range sig {
		switch ch {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				result.WriteRune(ch)
			}
		}
	}
	out := result.String()
	for strings.Contains(out, "  ") {
		out = strings.ReplaceAll(out, "  ", " ")
	}
	return strings.TrimSpace(out)
}
