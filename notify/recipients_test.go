package notify

import "testing"

func TestValidRecipients(t *testing.T) {
	tests := []struct {
		name string
		in   []Identity
		want []Identity
	}{
		{
			name: "nil is nil",
			in:   nil,
			want: nil,
		},
		{
			name: "drops empty subject",
			in:   []Identity{{Issuer: "osg", Subject: ""}, {Issuer: "osg", Subject: "user:1"}},
			want: []Identity{{Issuer: "osg", Subject: "user:1"}},
		},
		{
			name: "dedupes on issuer+subject",
			in: []Identity{
				{Issuer: "osg", Subject: "user:1", Label: "Ada"},
				{Issuer: "osg", Subject: "user:1", Label: "Ada (again)"},
				{Issuer: "osg", Subject: "user:2"},
			},
			want: []Identity{
				{Issuer: "osg", Subject: "user:1", Label: "Ada"},
				{Issuer: "osg", Subject: "user:2"},
			},
		},
		{
			name: "same subject, different issuer kept",
			in: []Identity{
				{Issuer: "osg", Subject: "user:1"},
				{Issuer: "other", Subject: "user:1"},
			},
			want: []Identity{
				{Issuer: "osg", Subject: "user:1"},
				{Issuer: "other", Subject: "user:1"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validRecipients(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d recipients, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("recipient[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
