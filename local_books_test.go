package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeBookmarkStore struct {
	bookmark readeckBookmark
	patches  []map[string]any
	gets     int
	patchErr error
}

func (store *fakeBookmarkStore) getBookmark(id string) (readeckBookmark, error) {
	store.gets++
	return store.bookmark, nil
}

func (store *fakeBookmarkStore) patchBookmark(id string, fields map[string]any) error {
	store.patches = append(store.patches, fields)
	if store.patchErr != nil {
		return store.patchErr
	}
	if progress, ok := fields["read_progress"].(int); ok {
		store.bookmark.ReadProgress = progress
	}
	if archived, ok := fields["is_archived"].(bool); ok {
		store.bookmark.IsArchived = archived
	}
	if marked, ok := fields["is_marked"].(bool); ok {
		store.bookmark.IsMarked = marked
	}
	return nil
}

type fakeNickelLibrary struct {
	status        bookStatus
	inCollection  bool
	statusErr     error
	collectionErr error
}

func (library fakeNickelLibrary) readStatus(id, outputDir string) (bookStatus, error) {
	return library.status, library.statusErr
}

func (library fakeNickelLibrary) isInCollection(id, outputDir, collection string) (bool, error) {
	return library.inCollection, library.collectionErr
}

type localBookSyncTest struct {
	t              *testing.T
	readeck        fakeBookmarkStore
	nickel         fakeNickelLibrary
	cfg            appConfig
	book           localBook
	keepLocal      bool
	listedBookmark bool
}

func newLocalBookSyncTest(t *testing.T) *localBookSyncTest {
	t.Helper()
	outputDir := t.TempDir()
	bookPath := filepath.Join(outputDir, nativeTestBookmarkID+".kepub.epub")
	if err := os.WriteFile(bookPath, []byte("native fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &localBookSyncTest{
		t:       t,
		readeck: fakeBookmarkStore{bookmark: readeckBookmark{ID: nativeTestBookmarkID}},
		nickel:  fakeNickelLibrary{status: bookUnread},
		cfg:     appConfig{Output: outputConfig{Path: outputDir}},
		book:    localBook{id: nativeTestBookmarkID, path: bookPath},
	}
}

func (h *localBookSyncTest) bookWasFetched()   { h.keepLocal = true; h.listedBookmark = true }
func (h *localBookSyncTest) archiveReadBooks() { h.cfg.Sync.Archive = true }
func (h *localBookSyncTest) syncFavourites() {
	h.cfg.Sync.FavouriteCollection = nativeTestFavouriteShelf
}
func (h *localBookSyncTest) bookIsInKoboCollection() { h.nickel.inCollection = true }
func (h *localBookSyncTest) deleteStaleFiles()       { h.cfg.Output.Delete = true }

func (h *localBookSyncTest) remoteBookmark(bookmark readeckBookmark) {
	if bookmark.ID == "" {
		bookmark.ID = nativeTestBookmarkID
	}
	h.readeck.bookmark = bookmark
}

type localBookSyncResult struct {
	state        readeckBookmark
	filesChanged bool
	patches      int
	gets         int
	removed      bool
}

func (h *localBookSyncTest) run(status bookStatus) localBookSyncResult {
	h.t.Helper()
	h.nickel.status = status
	keepLocal := make(map[string]bool)
	if h.keepLocal {
		keepLocal[nativeTestBookmarkID] = true
	}
	bookmarks := make(map[string]readeckBookmark)
	if h.listedBookmark {
		bookmarks[nativeTestBookmarkID] = h.readeck.bookmark
	}

	update, err := updateReadeckFromKobo(&h.readeck, h.nickel, h.cfg, h.cfg.Output.Path, h.book, bookmarks)
	if err != nil {
		h.t.Fatal(err)
	}
	filesChanged, err := removeStaleLocalBook(h.book, keepLocal[h.book.id] && !update.archivedInReadeck, h.cfg.Output.Delete, update.koboReadingStatus)
	if err != nil {
		h.t.Fatal(err)
	}
	_, statErr := os.Stat(h.book.path)
	return localBookSyncResult{state: h.readeck.bookmark, filesChanged: filesChanged, patches: len(h.readeck.patches), gets: h.readeck.gets, removed: os.IsNotExist(statErr)}
}

func requireReadeckState(t *testing.T, got readeckBookmark, readProgress int, archived, marked bool) {
	t.Helper()
	if got.ReadProgress != readProgress || got.IsArchived != archived || got.IsMarked != marked {
		t.Fatalf("unexpected Readeck state: %+v", got)
	}
}

func requireAPICalls(t *testing.T, got localBookSyncResult, patches, gets int) {
	t.Helper()
	if got.patches != patches || got.gets != gets {
		t.Fatalf("API calls: got %d PATCH and %d GET, want %d PATCH and %d GET", got.patches, got.gets, patches, gets)
	}
}

func requireLocalFile(t *testing.T, got localBookSyncResult, wantFile bool) {
	t.Helper()
	if wantFile && got.removed {
		t.Fatal("local book was removed")
	}
	if !wantFile && !got.removed {
		t.Fatal("local book was not removed")
	}
}

func TestListLocalBooksOnlyListsKepubsInOutputDirectory(t *testing.T) {
	// Important: Files outside Kobodeck's output directory must not become deletion candidates
	// Given
	outputDir := t.TempDir()
	kepubPath := filepath.Join(outputDir, "bookmark-1.kepub.epub")
	if err := os.WriteFile(kepubPath, []byte("kepub"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "ordinary.epub"), []byte("epub"), 0o600); err != nil {
		t.Fatal(err)
	}
	nestedDir := filepath.Join(outputDir, "nested")
	if err := os.Mkdir(nestedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "nested.kepub.epub"), []byte("kepub"), 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	books, err := listLocalBooks(outputDir)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("listLocalBooks returned %d books, want 1: %+v", len(books), books)
	}
	if books[0].id != "bookmark-1" || books[0].path != kepubPath {
		t.Fatalf("listLocalBooks returned %+v, want bookmark-1 at %s", books[0], kepubPath)
	}
}

func TestUpdateReadeckMarksFinishedBookmarkReadArchivedAndFavourited(t *testing.T) {
	// Given
	localBooks := newLocalBookSyncTest(t)
	localBooks.bookWasFetched()
	localBooks.archiveReadBooks()
	localBooks.syncFavourites()
	localBooks.bookIsInKoboCollection()
	// When
	got := localBooks.run(bookRead)
	// Then
	requireReadeckState(t, got.state, 100, true, true)
	requireAPICalls(t, got, 2, 0)
	requireLocalFile(t, got, true)
}

func TestUpdateReadeckMarksFinishedBookmarkReadWithoutArchiving(t *testing.T) {
	// Given
	localBooks := newLocalBookSyncTest(t)
	localBooks.bookWasFetched()
	localBooks.syncFavourites()
	// When
	got := localBooks.run(bookRead)
	// Then
	requireReadeckState(t, got.state, 100, false, false)
	requireAPICalls(t, got, 1, 0)
	requireLocalFile(t, got, true)
}

func TestUpdateReadeckMarksFinishedBookOutsideFetchedFeedRead(t *testing.T) {
	// Given
	localBooks := newLocalBookSyncTest(t)
	localBooks.archiveReadBooks()
	// When
	got := localBooks.run(bookRead)
	// Then
	requireReadeckState(t, got.state, 100, true, false)
	requireAPICalls(t, got, 1, 1)
	requireLocalFile(t, got, true)
}

func TestUpdateReadeckUnfavoursArchivedBookmark(t *testing.T) {
	// Given
	localBooks := newLocalBookSyncTest(t)
	localBooks.syncFavourites()
	localBooks.remoteBookmark(readeckBookmark{IsArchived: true, IsMarked: true})
	// When
	got := localBooks.run(bookUnread)
	// Then
	requireReadeckState(t, got.state, 0, true, false)
	requireAPICalls(t, got, 1, 1)
	requireLocalFile(t, got, true)
}

func TestRemoveStaleLocalBookDeletesUnreadBookmark(t *testing.T) {
	// Given
	localBooks := newLocalBookSyncTest(t)
	localBooks.deleteStaleFiles()
	// When
	got := localBooks.run(bookUnread)
	// Then
	requireLocalFile(t, got, false)
	if !got.filesChanged {
		t.Fatal("deleted stale bookmark was not reported as a filesystem change")
	}
}

func TestSyncLocalBooksDoesNotDeleteWhenDeletionIsDisallowed(t *testing.T) {
	// Given
	localBooks := newLocalBookSyncTest(t)
	localBooks.deleteStaleFiles()
	keepLocal := make(map[string]bool)
	bookmarks := make(map[string]readeckBookmark)
	// When
	filesChanged, err := syncLocalBooks(&localBooks.readeck, localBooks.nickel, localBooks.cfg, keepLocal, bookmarks, false)
	// Then
	if err != nil {
		t.Fatalf("syncLocalBooks: %v", err)
	}
	if _, err := os.Stat(localBooks.book.path); err != nil {
		t.Fatalf("stale local book was deleted: %v", err)
	}
	if filesChanged {
		t.Fatal("retained stale bookmark was reported as a filesystem change")
	}
}

func TestUpdateReadeckFromKoboPropagatesStatusError(t *testing.T) {
	// Given
	localBooks := newLocalBookSyncTest(t)
	localBooks.nickel.statusErr = errors.New("status unavailable")
	// When
	_, err := updateReadeckFromKobo(&localBooks.readeck, localBooks.nickel, localBooks.cfg, localBooks.cfg.Output.Path, localBooks.book, make(map[string]readeckBookmark))
	// Then
	if err == nil || !strings.Contains(err.Error(), "status unavailable") {
		t.Fatalf("status error = %v, want propagated status error", err)
	}
}

func TestUpdateReadeckFromKoboReportsMarkReadFailure(t *testing.T) {
	// Given
	localBooks := newLocalBookSyncTest(t)
	localBooks.bookWasFetched()
	localBooks.nickel.status = bookRead
	localBooks.readeck.patchErr = errors.New("remote update failed")
	// When
	_, err := updateReadeckFromKobo(&localBooks.readeck, localBooks.nickel, localBooks.cfg, localBooks.cfg.Output.Path, localBooks.book, map[string]readeckBookmark{nativeTestBookmarkID: localBooks.readeck.bookmark})
	// Then
	if err == nil || !strings.Contains(err.Error(), "remote update failed") {
		t.Fatalf("mark-read error = %v, want remote update error", err)
	}
}

func TestSyncLocalBooksPreservesLocalBookWhenMarkReadFails(t *testing.T) {
	// Given
	localBooks := newLocalBookSyncTest(t)
	localBooks.deleteStaleFiles()
	localBooks.nickel.status = bookRead
	remoteErr := errors.New("remote update failed")
	localBooks.readeck.patchErr = remoteErr
	// When
	filesChanged, err := syncLocalBooks(
		&localBooks.readeck,
		localBooks.nickel,
		localBooks.cfg,
		make(map[string]bool),
		make(map[string]readeckBookmark),
		true,
	)
	// Then
	if !errors.Is(err, remoteErr) {
		t.Fatalf("local book sync error = %v, want remote update error", err)
	}
	if filesChanged {
		t.Fatal("retained book was reported as a filesystem change")
	}
	if _, err := os.Stat(localBooks.book.path); err != nil {
		t.Fatalf("local retry input was not preserved: %v", err)
	}
}

func TestRemoveStaleLocalBookPreservesBookMissingFromFetchWhileReading(t *testing.T) {
	// Given
	localBooks := newLocalBookSyncTest(t)
	localBooks.deleteStaleFiles()
	// When
	got := localBooks.run(bookReading)
	// Then
	requireLocalFile(t, got, true)
	if got.filesChanged {
		t.Fatal("retained in-progress bookmark was reported as a filesystem change")
	}
}

func TestRemoveStaleLocalBookPreservesBookMissingFromFetchWhenClosed(t *testing.T) {
	// Given
	localBooks := newLocalBookSyncTest(t)
	localBooks.deleteStaleFiles()
	// When
	got := localBooks.run(bookClosed)
	// Then
	requireLocalFile(t, got, true)
	if got.filesChanged {
		t.Fatal("retained closed bookmark was reported as a filesystem change")
	}
}
