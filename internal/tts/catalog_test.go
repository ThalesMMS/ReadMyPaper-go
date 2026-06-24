package tts

import "testing"

func TestCatalogResolveDefaultsAndCompatibility(t *testing.T) {
	catalog := NewCatalog(t.TempDir())
	if got := catalog.Resolve("auto", "pt-BR", "piper"); got.Key != "pt_BR-faber-medium" {
		t.Fatalf("Piper pt default = %q", got.Key)
	}
	if got := catalog.Resolve("auto", "en", "kokoro"); got.Key != "kokoro-en-heart" {
		t.Fatalf("Kokoro en default = %q", got.Key)
	}
	if catalog.IsCompatible("kokoro-en-heart", "piper") {
		t.Fatal("Kokoro voice should not be Piper-compatible")
	}
	if !catalog.IsCompatible("auto", "piper") {
		t.Fatal("auto voice should be compatible")
	}
}

func TestPiperLengthScale(t *testing.T) {
	if got := PiperLengthScale(1.25); got != 0.8 {
		t.Fatalf("got %v", got)
	}
	if got := PiperLengthScale(10); got != 0.714 {
		t.Fatalf("rate clamp got %v", got)
	}
}
