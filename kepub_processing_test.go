package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const (
	nativeTestBookmarkID     = "bookmark-1"
	nativeTestFavouriteShelf = "Native Favourites"
)

// writeTestEPUB writes a minimal valid EPUB and returns its bytes so tests can
// verify that failed operations leave the original file unchanged.
func writeTestEPUB(t *testing.T, path string) []byte {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	mimetype, err := w.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mimetype.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}
	container, err := w.Create("META-INF/container.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := container.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`)); err != nil {
		t.Fatal(err)
	}
	content, err := w.Create("OEBPS/content.opf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := content.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="bookid">test</dc:identifier><dc:title>Test</dc:title><dc:language>en</dc:language></metadata>
  <manifest>
    <item id="chapter" href="chapter.xhtml" media-type="application/xhtml+xml"/>
    <item id="res-cover" href="cover.jpg" media-type="image/jpeg"/>
  </manifest>
  <spine><itemref idref="chapter"/></spine>
</package>`)); err != nil {
		t.Fatal(err)
	}
	chapter, err := w.Create("OEBPS/chapter.xhtml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chapter.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml"><head><title>Test</title></head><body><p>Hello</p></body></html>`)); err != nil {
		t.Fatal(err)
	}
	cover, err := w.Create("OEBPS/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cover.Write([]byte("test cover")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestToKepubAtomicallyInstallsConvertedBook(t *testing.T) {
	outputDir := t.TempDir()
	source := filepath.Join(outputDir, "article.epub")
	destination := filepath.Join(outputDir, "article.kepub.epub")
	writeTestEPUB(t, source)

	kepubPath, err := toKepub(source, destination)
	if err != nil {
		t.Fatalf("toKepub: %v", err)
	}
	if err := validateEPUB(kepubPath); err != nil {
		t.Fatalf("converted KEPUB is invalid: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source EPUB still exists: %v", err)
	}
	tmpFiles, err := filepath.Glob(filepath.Join(outputDir, ".article.kepub.epub.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpFiles) != 0 {
		t.Fatalf("conversion temporary files remain: %v", tmpFiles)
	}
}

func TestToKepubFailurePreservesExistingBook(t *testing.T) {
	outputDir := t.TempDir()
	source := filepath.Join(outputDir, "article.epub")
	final := filepath.Join(outputDir, "article.kepub.epub")
	if err := os.WriteFile(source, []byte("invalid source"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := writeTestEPUB(t, final)

	if _, err := toKepub(source, final); err == nil {
		t.Fatal("invalid source unexpectedly converted")
	}
	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatal("existing KEPUB was changed after failed conversion")
	}
}

func TestFixCoverRenameFailureRemovesTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "article.epub")
	original := writeTestEPUB(t, path)
	renameErr := errors.New("rename failed")

	err := fixCoverWithRename(path, func(oldPath, newPath string) error {
		if oldPath != path+".covertmp" || newPath != path {
			t.Errorf("rename paths = %q, %q", oldPath, newPath)
		}
		return renameErr
	})
	if !errors.Is(err, renameErr) {
		t.Fatalf("fixCoverWithRename error = %v, want rename error", err)
	}
	if _, err := os.Stat(path + ".covertmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cover temporary file remains: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatal("original EPUB changed after failed cover rename")
	}
}
