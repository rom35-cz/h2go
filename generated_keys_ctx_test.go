// generated_keys_ctx_test.go — unit tests for per-statement generated-keys
// overrides (post-v0.2.0 backlog item #7): context overrides must beat the
// connection-level Config for exactly one statement, unknown modes must fall
// back to the configuration, and the None escape hatch must be expressible.

package h2go

import (
	"context"
	"testing"
)

func TestResolveGeneratedKeysPrecedence(t *testing.T) {
	cfgNoneSet := &Config{
		GeneratedKeysMode:    GeneratedKeysNone,
		GeneratedKeysModeSet: true,
	}
	cfgCols := &Config{
		GeneratedKeysMode:    GeneratedKeysColumnNumbers,
		GeneratedKeysModeSet: true,
		GeneratedKeysColumns: []int{3},
	}

	tests := []struct {
		name      string
		cfg       *Config
		override  *GeneratedKeysRequest
		wantMode  int32
		wantCols  []int
		wantNames []string
	}{
		{"default config is auto", &Config{}, nil, GeneratedKeysAuto, nil, nil},
		{"config none set", cfgNoneSet, nil, GeneratedKeysNone, nil, nil},
		{"config columns", cfgCols, nil, GeneratedKeysColumnNumbers, []int{3}, nil},
		{"override none beats config auto", &Config{}, &GeneratedKeysRequest{Mode: GeneratedKeysNone}, GeneratedKeysNone, nil, nil},
		{"override auto beats config none", cfgNoneSet, &GeneratedKeysRequest{Mode: GeneratedKeysAuto}, GeneratedKeysAuto, nil, nil},
		{"override columns with values", &Config{}, &GeneratedKeysRequest{Mode: GeneratedKeysColumnNumbers, Columns: []int{1, 2}}, GeneratedKeysColumnNumbers, []int{1, 2}, nil},
		{"override names", &Config{}, &GeneratedKeysRequest{Mode: GeneratedKeysColumnNames, Names: []string{"ID"}}, GeneratedKeysColumnNames, nil, []string{"ID"}},
		{"unknown override falls back to config", cfgCols, &GeneratedKeysRequest{Mode: 99}, GeneratedKeysColumnNumbers, []int{3}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{cfg: tc.cfg}
			ctx := context.Background()
			if tc.override != nil {
				ctx = ContextWithGeneratedKeys(ctx, *tc.override)
			}
			gotMode, gotCols, gotNames := s.resolveGeneratedKeys(ctx)
			if gotMode != tc.wantMode {
				t.Errorf("mode = %d, want %d", gotMode, tc.wantMode)
			}
			if len(gotCols) != len(tc.wantCols) {
				t.Errorf("cols = %v, want %v", gotCols, tc.wantCols)
			} else {
				for i := range gotCols {
					if gotCols[i] != tc.wantCols[i] {
						t.Errorf("cols = %v, want %v", gotCols, tc.wantCols)
						break
					}
				}
			}
			if len(gotNames) != len(tc.wantNames) {
				t.Errorf("names = %v, want %v", gotNames, tc.wantNames)
			} else {
				for i := range gotNames {
					if gotNames[i] != tc.wantNames[i] {
						t.Errorf("names = %v, want %v", gotNames, tc.wantNames)
						break
					}
				}
			}
		})
	}
}

func TestContextWithoutGeneratedKeys(t *testing.T) {
	s := &Session{cfg: &Config{}}
	ctx := ContextWithoutGeneratedKeys(context.Background())
	mode, _, _ := s.resolveGeneratedKeys(ctx)
	if mode != GeneratedKeysNone {
		t.Errorf("mode = %d, want GeneratedKeysNone (%d)", mode, GeneratedKeysNone)
	}
}

func TestGeneratedKeysOverrideWrongTypeIgnored(t *testing.T) {
	s := &Session{cfg: &Config{}}
	ctx := context.WithValue(context.Background(), genKeysCtxKey{}, "not-a-request")
	mode, _, _ := s.resolveGeneratedKeys(ctx)
	if mode != GeneratedKeysAuto {
		t.Errorf("mode = %d, want auto fallback", mode)
	}
}
