package option

import (
	"strings"
	"testing"

	C "github.com/singlink/singlink/constant"
)

func TestOptionsRejectsDuplicateV2BoardNodeTagsAcrossServices(t *testing.T) {
	err := checkOptions(&Options{
		Services: []Service{
			{
				Type: C.TypeV2Board,
				Tag:  "panel-a",
				Options: &V2BoardServiceOptions{
					Nodes: []V2BoardNodeOptions{{
						Tag:      "shared-node",
						NodeID:   1,
						NodeType: C.TypeVLESS,
					}},
				},
			},
			{
				Type: C.TypeV2Board,
				Tag:  "panel-b",
				Options: &V2BoardServiceOptions{
					Nodes: []V2BoardNodeOptions{{
						Tag:      "shared-node",
						NodeID:   2,
						NodeType: C.TypeVLESS,
					}},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate v2board node tag: shared-node") {
		t.Fatalf("expected duplicate v2board node tag error, got %v", err)
	}
}

func TestOptionsRejectsDuplicateDefaultV2BoardNodeTagsForTaglessServices(t *testing.T) {
	err := checkOptions(&Options{
		Services: []Service{
			{
				Type: C.TypeV2Board,
				Options: &V2BoardServiceOptions{
					Nodes: []V2BoardNodeOptions{{
						NodeID:   1,
						NodeType: C.TypeVLESS,
					}},
				},
			},
			{
				Type: C.TypeV2Board,
				Options: &V2BoardServiceOptions{
					Nodes: []V2BoardNodeOptions{{
						NodeID:   1,
						NodeType: C.TypeVLESS,
					}},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate v2board node tag: -vless-1") {
		t.Fatalf("expected duplicate default v2board node tag error, got %v", err)
	}
}
