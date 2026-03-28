# 🖼️ Media & Assets — v0 Specification (CDN-Ready, Extensible)

## 1. Overview

Media system handles:

* file uploads (images, files)
* storage abstraction (local, S3, CDN)
* asset referencing in domain (products, etc.)
* URL generation

Design goals:

* simple default (local filesystem)
* pluggable storage backends
* CDN-ready
* no vendor lock-in

---

## 2. Core Concepts

---

### 2.1 Asset

Represents a stored file.

```go id="8c1lq3"
type Asset struct {
    ID        string

    Path      string
    Filename  string
    MimeType  string

    Size      int64

    Meta      map[string]interface{}

    CreatedAt time.Time
}
```

---

👉 Important:

* `Path` is storage-relative (not full URL)
* URL is generated dynamically

---

## 3. Storage Interface

---

```go id="y0h9az"
type Storage interface {
    Name() string

    Save(path string, file io.Reader) error

    Delete(path string) error

    URL(path string) string
}
```

---

---

## 4. Default Implementation (Core)

---

### Local Storage

```go id="7t4m9r"
type LocalStorage struct {
    BasePath string // e.g. ./public/media
    BaseURL  string // e.g. /media
}
```

---

Behavior:

* saves files to disk
* serves via HTTP

---

---

## 5. Upload Flow

---

### Endpoint:

```http id="3o5jmx"
POST /media/upload
```

---

### Flow:

1. receive file
2. generate path (UUID or hash)
3. call storage.Save()
4. create Asset record
5. return asset

---

---

## 6. URL Generation

---

Always via storage:

```go id="l8k2n0"
url := storage.URL(asset.Path)
```

---

Examples:

---

### Local

```text id="6r3yt0"
/media/products/abc.jpg
```

---

### CDN (plugin)

```text id="wn0m6g"
https://cdn.example.com/products/abc.jpg
```

---

---

## 7. Storage Backends (Plugins)

---

Examples:

---

### S3 Storage

```go id="6fcbm6"
type S3Storage struct{}
```

---

### CDN Wrapper

```go id="q9c6e0"
type CDNStorage struct {
    Base Storage
}
```

---

---

## 8. Configuration

---

```yaml id="5hz3fh"
media:
  storage: local
```

---

---

## 9. File Organization

---

Recommended:

```text id="f8nq5g"
/media
  /products
  /categories
  /uploads
```

---

---

## 10. Product Integration

---

Products reference assets:

```go id="q6k3nh"
type Product struct {
    ID string

    ImageIDs []string
}
```

---

Assets resolved via:

* composition pipeline

---

---

## 11. Image Variants (Future)

---

Examples:

* thumbnail
* medium
* large

---

Handled via:

* plugin or background worker

---

---

## 12. Security

---

* validate file type
* limit file size
* prevent path traversal

---

---

## 13. Events

---

* `asset.uploaded`
* `asset.deleted`

---

---

## 14. Cleanup

---

Optional:

* orphan cleanup job
* unused asset detection

---

---

## 15. Extensibility

---

Plugins can:

* replace storage
* add transformations
* add metadata extraction

---

Example:

```go id="k2r3pm"
RegisterStorage("s3", S3Storage{})
```

---

---

## 16. Performance

---

* local storage for dev/small setups
* CDN for production
* caching headers (future)

---

---

## 17. Non-Goals (v0)

---

* no video streaming
* no advanced DAM features
* no automatic optimization
* no versioning

---

---

## 18. Summary

Media system v0 provides:

> a simple, pluggable storage system that supports local development and scales to CDN-backed production setups.

It ensures:

* minimal setup
* flexibility
* future scalability

---
