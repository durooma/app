package eval

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
)

// Suite paths are relative to the manifest. Every member is required. Matching
// IDs may repeat only with identical content and provenance (the smoke subset).
type Suite struct {
	Name     string `json:"name"`
	Datasets []struct {
		Path   string `json:"path"`
		Source string `json:"source"`
	} `json:"datasets"`
}

func LoadSuite(path string) (Dataset, string, error) {
	var s Suite
	if _, err := decodeFile(path, &s); err != nil {
		return Dataset{}, "", err
	}
	combined := Dataset{Name: s.Name}
	seen := map[string]Example{}
	for _, member := range s.Datasets {
		if member.Source == "" || member.Path == "" {
			return combined, "", fmt.Errorf("suite members need path and source")
		}
		p := member.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(path), p)
		}
		d, _, err := LoadDataset(p)
		if err != nil {
			return combined, "", err
		}
		if combined.Categories == nil {
			combined.Categories = d.Categories
		} else if !reflect.DeepEqual(combined.Categories, d.Categories) {
			return combined, "", fmt.Errorf("%s: suite category definitions differ", p)
		}
		for _, ex := range d.Examples {
			if ex.Source != "" && ex.Source != member.Source {
				return combined, "", fmt.Errorf("%s: source conflicts with suite", ex.ID)
			}
			ex.Source = member.Source
			if old, ok := seen[ex.ID]; ok {
				if old != ex {
					return combined, "", fmt.Errorf("conflicting duplicate ID %q", ex.ID)
				}
				continue
			}
			seen[ex.ID] = ex
			combined.Examples = append(combined.Examples, ex)
		}
	}
	if err := combined.Validate(); err != nil {
		return combined, "", err
	}
	b, err := json.Marshal(combined)
	if err != nil {
		return combined, "", err
	}
	return combined, fmt.Sprintf("%x", sha256.Sum256(b)), nil
}
