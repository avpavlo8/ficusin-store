package reviews

import "testing"

func TestValidMediaSignature(t *testing.T) {
	tests := []struct { name, contentType string; data []byte; want bool }{
		{"mp4", "video/mp4", []byte{0, 0, 0, 20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, true},
		{"webm", "video/webm", []byte{0x1a, 0x45, 0xdf, 0xa3}, true},
		{"fake mp4", "video/mp4", []byte("not a video payload"), false},
		{"unsupported", "video/quicktime", []byte{0, 0, 0, 20, 'f', 't', 'y', 'p'}, false},
	}
	for _, test := range tests { t.Run(test.name, func(t *testing.T) { if got := validMediaSignature(test.contentType, test.data); got != test.want { t.Fatalf("validMediaSignature() = %v, want %v", got, test.want) } }) }
}
