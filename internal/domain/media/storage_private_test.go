package media_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
)

func TestPrivatePathHelpers(t *testing.T) {
	t.Parallel()
	if !media.IsPrivateDownloadsPath("private/downloads/x") {
		t.Fatal("downloads")
	}
	if !media.IsPrivateBlobsPath("/private/blobs/y") {
		t.Fatal("blobs")
	}
	if !media.IsPrivatePath("private/blobs/z") || media.IsPrivatePath("public/x") {
		t.Fatal("IsPrivatePath")
	}
}

func TestAsSignedURLStorage_Nil(t *testing.T) {
	t.Parallel()
	if _, ok := media.AsSignedURLStorage(nil); ok {
		t.Fatal("want false")
	}
}
