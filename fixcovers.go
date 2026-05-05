package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	stdDraw "image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
)

const (
	coverWidth  = 600
	coverHeight = 800
)

// fixCover injects a standardised cover into the EPUB at path, overwriting it
// in place. It picks the best available source image (first JPEG/PNG content
// image, falling back to the site favicon), scales it to a 600×800 portrait
// canvas, and declares it via EPUB3 properties="cover-image" plus an EPUB2
// <meta name="cover"> fallback. A cover.xhtml wrapper is added to the manifest
// and guide but NOT inserted into the reading spine, so opening the book lands
// directly on the article.
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
		return nil // no image available, leave unchanged
	}

	srcData, err := readCoverZipEntry(r, "OEBPS/"+source.Href)
	if err != nil {
		return fmt.Errorf("read source image: %w", err)
	}

	var patchedOPF []byte
	extras := map[string][]byte{}

	coverData, err := makeCoverJPEG(srcData)
	if err != nil {
		// Unprocessable image (e.g. SVG): declare existing item as cover.
		patchedOPF = addCoverMetaOnly(opfData, source.ID)
	} else {
		const (
			coverID       = "cover"
			coverHref     = "Images/cover.jpg"
			coverPageID   = "cover-page"
			coverPageHref = "cover.xhtml"
		)
		patchedOPF = addCoverDeclarations(opfData, coverID, coverHref, coverPageID, coverPageHref)
		extras["OEBPS/"+coverHref] = coverData
		extras["OEBPS/"+coverPageHref] = makeCoverPage(coverHref)
		log.Printf("  cover fixed: %s -> %s", filepath.Base(path), source.Href)
	}

	tmp := path + ".covertmp"
	if err := writeCoverEPUB(r, tmp, opfPath, patchedOPF, extras); err != nil {
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

func makeCoverJPEG(src []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}

	srcW := img.Bounds().Dx()
	srcH := img.Bounds().Dy()

	scaleW := float64(coverWidth) / float64(srcW)
	scaleH := float64(coverHeight) / float64(srcH)
	scale := min(scaleW, scaleH)
	scaledW := max(1, int(float64(srcW)*scale))
	scaledH := max(1, int(float64(srcH)*scale))

	canvas := image.NewNRGBA(image.Rect(0, 0, coverWidth, coverHeight))
	stdDraw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, stdDraw.Src)

	offsetX := (coverWidth - scaledW) / 2
	offsetY := (coverHeight - scaledH) / 2
	dstRect := image.Rect(offsetX, offsetY, offsetX+scaledW, offsetY+scaledH)
	xdraw.CatmullRom.Scale(canvas, dstRect, img, img.Bounds(), xdraw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func makeCoverPage(coverHref string) []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
  <head>
    <title>Cover</title>
  </head>
  <body>
    <section epub:type="cover">
      <img src="%s" alt="Cover" style="height: 100%%"/>
    </section>
  </body>
</html>`, coverHref))
}

// addCoverDeclarations patches the OPF with EPUB3 properties="cover-image" on
// the image manifest item, an EPUB2 <meta name="cover"> for compatibility, a
// cover.xhtml manifest entry, and a <guide> reference. The cover page is NOT
// added to the spine so opening the book lands directly on the article.
func addCoverDeclarations(opf []byte, coverID, coverHref, coverPageID, coverPageHref string) []byte {
	s := string(opf)

	imgItem := fmt.Sprintf(`    <item id="%s" href="%s" media-type="image/jpeg" properties="cover-image"/>`, coverID, coverHref)
	pageItem := fmt.Sprintf(`    <item id="%s" href="%s" media-type="application/xhtml+xml"/>`, coverPageID, coverPageHref)
	s = strings.Replace(s, "</manifest>", imgItem+"\n    "+pageItem+"\n  </manifest>", 1)

	meta := fmt.Sprintf(`    <meta name="cover" content="%s"/>`, coverID)
	s = strings.Replace(s, "</metadata>", meta+"\n  </metadata>", 1)

	guide := fmt.Sprintf("  <guide>\n    <reference type=\"cover\" title=\"Cover\" href=\"%s\"/>\n  </guide>\n", coverPageHref)
	s = strings.Replace(s, "</package>", guide+"</package>", 1)

	return []byte(s)
}

func addCoverMetaOnly(opf []byte, coverID string) []byte {
	s := string(opf)
	meta := fmt.Sprintf(`    <meta name="cover" content="%s"/>`, coverID)
	return []byte(strings.Replace(s, "</metadata>", meta+"\n  </metadata>", 1))
}

func writeCoverEPUB(r *zip.ReadCloser, dst, opfPath string, patchedOPF []byte, extras map[string][]byte) error {
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

	for path, data := range extras {
		if err := writeCoverZipBytes(w, path, data, zip.Deflate); err != nil {
			return err
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
