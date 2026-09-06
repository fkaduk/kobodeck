package main

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/pgaskin/kepubify/v4/kepub"
)

// fixCover patches the OPF inside the EPUB at path to declare an existing image
// as the cover via EPUB3 properties="cover-image" and EPUB2 <meta name="cover">.
// Prefers the first res-* JPEG/PNG content image, falls back to icon-* favicon.
// This will show that image as article cover on the Kobo.
func fixCover(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()

	const opfPath = "OEBPS/content.opf"

	opfData, err := readCoverZipEntry(r, opfPath)
	if err != nil {
		return fmt.Errorf("read OPF: %w", err)
	}

	items, err := parseCoverManifest(opfData)
	if err != nil {
		return err
	}

	source := findFirstContentImage(items)
	if source == nil {
		source = findIconItem(items)
	}
	if source == nil {
		log.Printf("warning: no image available, leaving unchanged")
		return nil
	}

	patchedOPF := addCoverToOPF(opfData, source.ID)
	log.Printf("  cover fixed: %s -> %s", filepath.Base(path), source.Href)

	tmp := path + ".covertmp"
	if err := writeCoverEPUB(r, tmp, opfPath, patchedOPF); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

type coverManifestItem struct {
	ID        string `xml:"id,attr"`
	Href      string `xml:"href,attr"`
	MediaType string `xml:"media-type,attr"`
}

type coverOPFDoc struct {
	Items []coverManifestItem `xml:"manifest>item"`
}

func parseCoverManifest(opf []byte) ([]coverManifestItem, error) {
	var doc coverOPFDoc
	if err := xml.Unmarshal(opf, &doc); err != nil {
		return nil, fmt.Errorf("parse OPF: %w", err)
	}
	return doc.Items, nil
}

func findFirstContentImage(items []coverManifestItem) *coverManifestItem {
	for i := range items {
		if !strings.HasPrefix(items[i].ID, "res-") {
			continue
		}
		switch items[i].MediaType {
		case "image/jpeg", "image/png":
			return &items[i]
		}
	}
	return nil
}

func findIconItem(items []coverManifestItem) *coverManifestItem {
	for i := range items {
		if strings.HasPrefix(items[i].ID, "icon-") {
			return &items[i]
		}
	}
	return nil
}

// addCoverToOPF patches the OPF to declare the item with coverID as the cover:
// adds properties="cover-image" to the existing manifest item and inserts an
// EPUB2 <meta name="cover"> in the metadata.
func addCoverToOPF(opf []byte, coverID string) []byte {
	s := string(opf)
	s = strings.Replace(s, `id="`+coverID+`"`, `id="`+coverID+`" properties="cover-image"`, 1)
	meta := fmt.Sprintf(`    <meta name="cover" content="%s"/>`, coverID)
	s = strings.Replace(s, "</metadata>", meta+"\n  </metadata>", 1)
	return []byte(s)
}

func writeCoverEPUB(r *zip.ReadCloser, dst, opfPath string, patchedOPF []byte) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)

	mimetypeData, err := readCoverZipEntry(r, "mimetype")
	if err != nil {
		return err
	}
	if err := writeCoverZipBytes(w, "mimetype", mimetypeData, zip.Store); err != nil {
		return err
	}

	for _, entry := range r.File {
		switch entry.Name {
		case "mimetype":
		case opfPath:
			if err := writeCoverZipBytes(w, opfPath, patchedOPF, zip.Deflate); err != nil {
				return err
			}
		default:
			if err := copyCoverZipEntry(w, entry); err != nil {
				return err
			}
		}
	}

	return w.Close()
}

func readCoverZipEntry(r *zip.ReadCloser, name string) ([]byte, error) {
	for _, f := range r.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("%s: not found in zip", name)
}

func writeCoverZipBytes(w *zip.Writer, name string, data []byte, method uint16) error {
	h := &zip.FileHeader{Name: name, Method: method}
	ew, err := w.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = ew.Write(data)
	return err
}

func copyCoverZipEntry(w *zip.Writer, src *zip.File) error {
	ew, err := w.CreateHeader(&src.FileHeader)
	if err != nil {
		return err
	}
	rc, err := src.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(ew, rc)
	return err
}

// toKepub converts the EPUB at epubPath to kepubPath. The converted content is
// written and validated in a temporary file before it is atomically renamed
// into place. The original is removed after a successful conversion.
func toKepub(epubPath, kepubPath string) (string, error) {
	r, err := zip.OpenReader(epubPath)
	if err != nil {
		return "", err
	}
	defer r.Close()

	f, err := os.CreateTemp(filepath.Dir(kepubPath), "."+filepath.Base(kepubPath)+".tmp-*")
	if err != nil {
		return "", err
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	c := kepub.NewConverterWithOptions(kepub.ConverterOptionDummyTitlepage(false))
	convertErr := c.Convert(context.Background(), f, &r.Reader)
	syncErr := f.Sync()
	closeErr := f.Close()
	if convertErr != nil || syncErr != nil || closeErr != nil {
		os.Remove(epubPath)
		if convertErr != nil {
			return "", convertErr
		}
		if syncErr != nil {
			return "", syncErr
		}
		return "", closeErr
	}
	if err := validateEPUB(tmpPath); err != nil {
		os.Remove(epubPath)
		return "", fmt.Errorf("validate converted KEPUB: %w", err)
	}
	if err := os.Rename(tmpPath, kepubPath); err != nil {
		return "", err
	}
	if err := syncDirectory(filepath.Dir(kepubPath)); err != nil {
		log.Printf("warning: sync output directory %s: %v", filepath.Dir(kepubPath), err)
	}
	os.Remove(epubPath)
	return kepubPath, nil
}

// validateEPUB checks that path is a readable EPUB/KEPUB archive with the
// required mimetype entry.
func validateEPUB(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, entry := range r.File {
		if entry.Name != "mimetype" {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if string(data) != "application/epub+zip" {
			return fmt.Errorf("mimetype is %q", string(data))
		}
		return nil
	}
	return fmt.Errorf("mimetype entry not found")
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
