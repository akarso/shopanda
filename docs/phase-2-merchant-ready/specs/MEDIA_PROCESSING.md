# 🖼️ Media Processing — Specification

## 1. Overview

Extends the existing media system with:

* image resize and thumbnail generation
* WebP conversion and optimization
* S3-compatible cloud storage backend

Design goals:

* process on upload (no lazy generation)
* preset-based thumbnails (predictable URLs)
* pluggable storage (local → S3 without code changes)
* backward-compatible with existing `Storage` interface

---

## 2. Image Processing (PR-213)

---

### 2.1 Processor Interface

```go
type ImageProcessor interface {
    Resize(input io.Reader, opts ResizeOpts) (io.Reader, error)
    Format(input io.Reader, format string, quality int) (io.Reader, error)
}
```

---

### 2.2 Resize Options

```go
type ResizeOpts struct {
    Width   int
    Height  int
    Fit     string // "cover", "contain", "fill"
    Quality int    // 1-100
}
```

---

### 2.3 Thumbnail Presets

Configurable in `config.yaml`:

```yaml
media:
  thumbnails:
    small:  { width: 150, height: 150, fit: cover }
    medium: { width: 400, height: 400, fit: contain }
    large:  { width: 800, height: 800, fit: contain }
```

---

### 2.4 Upload Flow (Extended)

```text
1. Receive file upload
2. Validate MIME type (image/jpeg, image/png, image/webp, image/gif)
3. Store original via Storage.Save()
4. For each thumbnail preset:
   a. Resize image
   b. Save as {path}/{preset}/{filename}
5. Create Asset record with thumbnail paths
6. Return asset with URLs for all sizes
```

---

### 2.5 Asset Response (Extended)

```json
{
  "id": "asset_123",
  "url": "/media/abc123.jpg",
  "thumbnails": {
    "small": "/media/abc123/small.jpg",
    "medium": "/media/abc123/medium.jpg",
    "large": "/media/abc123/large.jpg"
  },
  "mime_type": "image/jpeg",
  "size": 245000
}
```

---

### 2.6 Implementation

Use pure Go library (e.g., `disintegration/imaging`) — no CGO, no external dependencies.

---

## 3. WebP Conversion (PR-214)

---

### 3.1 Conversion on Upload

For JPEG and PNG uploads:

```text
1. Generate original-format thumbnails (as PR-213)
2. Also generate WebP variants for each preset
3. Store both: {path}/{preset}/{filename}.jpg + {path}/{preset}/{filename}.webp
```

---

### 3.2 Configuration

```yaml
media:
  webp:
    enabled: true
    quality: 80
```

---

### 3.3 URL Pattern

Asset response includes WebP URLs:

```json
{
  "thumbnails": {
    "small": "/media/abc123/small.jpg",
    "small_webp": "/media/abc123/small.webp"
  }
}
```

Frontend templates can use `<picture>` with `<source type="image/webp">`.

---

## 4. S3 Storage Adapter (PR-215)

---

### 4.1 Implementation

```go
type S3Storage struct {
    client   *s3.Client
    bucket   string
    region   string
    baseURL  string // CDN URL or S3 public URL
}

func (s *S3Storage) Name() string { return "s3" }
func (s *S3Storage) Save(path string, file io.Reader) error
func (s *S3Storage) Delete(path string) error
func (s *S3Storage) URL(path string) string
```

---

### 4.2 Configuration

```yaml
media:
  storage: s3
  s3:
    endpoint: https://s3.amazonaws.com    # or MinIO URL
    bucket: shopanda-media
    region: eu-west-1
    access_key: ${SHOPANDA_MEDIA_S3_ACCESS_KEY}
    secret_key: ${SHOPANDA_MEDIA_S3_SECRET_KEY}
    base_url: https://cdn.example.com     # optional CDN URL
```

---

### 4.3 Compatibility

* Implements existing `Storage` interface — drop-in replacement
* Works with S3-compatible APIs (AWS S3, MinIO, DigitalOcean Spaces, Backblaze B2)
* `base_url` allows CDN in front of S3

---

### 4.4 Upload Flow

```text
1. Image processing (resize, WebP) happens in memory
2. Processed bytes uploaded to S3 via PutObject
3. Content-Type set from MIME detection
4. Cache-Control header: public, max-age=31536000
```

---

## 5. Migration Path

---

```text
Local storage (default, Phase 1)
  → Add image processing (PR-213, still local)
    → Add WebP (PR-214, still local)
      → Switch to S3 (PR-215, change config only)
```

Existing assets remain accessible. No migration tool needed — new uploads go to new storage, old URLs still work via local fallback.

---

## 6. Non-Goals (v0)

* No lazy/on-the-fly image generation
* No image CDN proxy (like Imgix or Cloudinary)
* No video processing
* No SVG optimization
* No bulk re-processing of existing assets
