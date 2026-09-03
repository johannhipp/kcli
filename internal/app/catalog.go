package app

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
)

type SideEffect string

const (
	SideEffectNone            SideEffect = "none"
	SideEffectLocal           SideEffect = "local"
	SideEffectAccountState    SideEffect = "account-state"
	SideEffectExternalMessage SideEffect = "external-message"
)

type Evidence string

const (
	EvidenceLive        Evidence = "live"
	EvidenceSource      Evidence = "source"
	EvidenceProvisional Evidence = "provisional"
	EvidenceLocal       Evidence = "local"
	EvidenceGap         Evidence = "gap"
)

type OperationMeta struct {
	Path                 string       `json:"path"`
	Purpose              string       `json:"purpose"`
	InputName            string       `json:"input"`
	OutputName           string       `json:"output"`
	SchemaVersion        string       `json:"schema_version"`
	AuthRequired         bool         `json:"auth_required"`
	SideEffect           SideEffect   `json:"side_effect"`
	ConfirmationRequired bool         `json:"confirmation_required"`
	Evidence             Evidence     `json:"evidence"`
	DefaultLimit         int          `json:"default_limit,omitempty"`
	HardLimit            int          `json:"hard_limit,omitempty"`
	Examples             []string     `json:"examples"`
	StoryIDs             []string     `json:"story_ids"`
	InputType            reflect.Type `json:"-"`
	OutputType           reflect.Type `json:"-"`
}

type Describer interface{ Describe() OperationMeta }
type Catalog struct{ operations []OperationMeta }

func BuildCatalog(model *kong.Application) (*Catalog, error) {
	if model == nil || model.Node == nil {
		return nil, fmt.Errorf("Kong model is required")
	}
	var operations []OperationMeta
	var walk func(*kong.Node) error
	walk = func(node *kong.Node) error {
		if node.Type == kong.CommandNode && node.Leaf() {
			describer, ok := targetDescriber(node.Target)
			if !ok {
				return fmt.Errorf("command %s does not implement Describe", nodePath(node))
			}
			meta := describer.Describe()
			meta.Path = nodePath(node)
			if err := validateMeta(meta); err != nil {
				return fmt.Errorf("command %s: %w", meta.Path, err)
			}
			operations = append(operations, meta)
		}
		for _, child := range node.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(model.Node); err != nil {
		return nil, err
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].Path < operations[j].Path })
	return &Catalog{operations: operations}, nil
}
func targetDescriber(target reflect.Value) (Describer, bool) {
	if !target.IsValid() {
		return nil, false
	}
	if target.CanInterface() {
		if value, ok := target.Interface().(Describer); ok {
			return value, true
		}
	}
	if target.CanAddr() && target.Addr().CanInterface() {
		value, ok := target.Addr().Interface().(Describer)
		return value, ok
	}
	return nil, false
}
func nodePath(node *kong.Node) string {
	var parts []string
	for current := node; current != nil && current.Type != kong.ApplicationNode; current = current.Parent {
		if current.Type == kong.CommandNode {
			parts = append(parts, current.Name)
		}
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return strings.Join(parts, " ")
}
func validateMeta(meta OperationMeta) error {
	if meta.Purpose == "" || meta.InputType == nil || meta.OutputType == nil || meta.InputName == "" || meta.OutputName == "" || meta.SchemaVersion == "" {
		return fmt.Errorf("incomplete operation metadata")
	}
	switch meta.SideEffect {
	case SideEffectNone, SideEffectLocal, SideEffectAccountState, SideEffectExternalMessage:
	default:
		return fmt.Errorf("invalid side-effect class")
	}
	switch meta.Evidence {
	case EvidenceLive, EvidenceSource, EvidenceProvisional, EvidenceLocal, EvidenceGap:
	default:
		return fmt.Errorf("invalid evidence")
	}
	if meta.ConfirmationRequired && meta.SideEffect != SideEffectExternalMessage {
		return fmt.Errorf("confirmation requires external-message side effect")
	}
	return nil
}
func (c *Catalog) Operations() []OperationMeta { return append([]OperationMeta(nil), c.operations...) }
func (c *Catalog) Find(path string) (OperationMeta, bool) {
	path = strings.Join(strings.Fields(path), " ")
	i := sort.Search(len(c.operations), func(i int) bool { return c.operations[i].Path >= path })
	if i < len(c.operations) && c.operations[i].Path == path {
		return c.operations[i], true
	}
	return OperationMeta{}, false
}
