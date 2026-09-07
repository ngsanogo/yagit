package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The build's one silent failure: a stylesheet that carries an asset inside
// it. Nothing fails, nothing is logged, and the released binary draws a
// fallback face — because the Content-Security-Policy the daemon sends forbids
// data: and only the built CSS ever contains one. It cost a font once.

func TestInlinedAsset(t *testing.T) {
	cases := []struct {
		name       string
		stylesheet string
		want       string
	}{
		{
			name:       "assets that are files",
			stylesheet: `@font-face{src:url(/assets/jetbrains-mono-abc.woff2) format("woff2")}`,
			want:       "",
		},
		{
			name:       "a font the bundler inlined",
			stylesheet: `@font-face{src:url(data:font/woff2;base64,d09GMgABAAAAAA) format("woff2")}`,
			want:       "data:font/woff2;base64,d09GMgABAAAAAA",
		},
		{
			// Both spellings are legal CSS and bundlers emit either, so a check
			// that knew only the bare one would pass the day it changed.
			name:       "a quoted data URI",
			stylesheet: `.icon{background-image:url("data:image/svg+xml,%3Csvg%3E")}`,
			want:       "data:image/svg+xml,%3Csvg%3E",
		},
		{
			name:       "the word data outside a url()",
			stylesheet: `.chart::after{content:"data: none"}`,
			want:       "",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := inlinedAsset(testCase.stylesheet); got != testCase.want {
				t.Errorf("inlinedAsset() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// A base64 payload runs to thousands of characters and says nothing. The
// error has to stay readable, and the media type at the front is what names
// the asset that slipped through.
func TestInlinedAssetShortensThePayload(t *testing.T) {
	got := inlinedAsset(`src:url(data:font/woff2;base64,` + strings.Repeat("A", 4000) + `)`)

	if len([]rune(got)) > 64 {
		t.Errorf("inlinedAsset() is %d runes long: %q", len([]rune(got)), got)
	}
	if !strings.HasPrefix(got, "data:font/woff2;base64,") {
		t.Errorf("inlinedAsset() = %q, want it to keep the media type", got)
	}
}

func TestVerifyAssetsAreFiles(t *testing.T) {
	project := newProject(t)
	assets := filepath.Join(project.directory, "internal", "assets", "dist", "assets")

	writeFile(t, filepath.Join(assets, "index-abc.css"), "body{color:#111}\n")
	writeFile(t, filepath.Join(assets, "index-abc.js"), `const uri = "data:text/plain,ok";`)

	if err := project.verifyAssetsAreFiles(); err != nil {
		t.Fatalf("a build with no inlined asset was refused: %v", err)
	}

	writeFile(t, filepath.Join(assets, "fonts-def.css"),
		`@font-face{src:url(data:font/woff2;base64,d09GMg)}`)

	err := project.verifyAssetsAreFiles()
	if err == nil {
		t.Fatal("a stylesheet carrying a data: URI was accepted")
	}
	// The message has to name the file, or the next person greps a build
	// output for a font they cannot find.
	if !strings.Contains(err.Error(), "fonts-def.css") {
		t.Errorf("error = %q, want it to name the stylesheet", err)
	}
	if !strings.Contains(err.Error(), "assetsInlineLimit") {
		t.Errorf("error = %q, want it to name the setting that prevents this", err)
	}
}
