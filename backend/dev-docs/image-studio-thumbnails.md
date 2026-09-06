# Image Studio thumbnails

New originals receive a separate WebP derivative, with the longest edge capped
at 768 pixels, quality 78, preserved aspect ratio and alpha. Encoding uses the
CGo-free libwebp implementation in `gen2brain/webp`; the deployment does not need
an external image service or system codec installation.
Static Linux builds must include the `nodynamic` build tag: CGo-free dynamic
library discovery otherwise adds a glibc loader requirement, incompatible with
the Alpine runtime. Docker, GoReleaser and the backend Makefile set this tag.

`thumbnail_ready` is recorded only after upload succeeds. The object key is the
original key plus `.thumb-v1.webp`; URLs are resolved through the original's
retained storage profile. Migration verifies both objects, and deletion removes
both from every recorded location. Original bytes and billing are unchanged.

The list, reference stacks and preview filmstrip fetch thumbnails near the
viewport. The preview loads an original when selected and retains visited
originals while switching within the batch.

To backfill existing files, build `./cmd/image-studio-thumbnails` locally with
`CGO_ENABLED=0 go build -tags=nodynamic` for the
target architecture, copy the binary into the running application container,
and run it with the same environment/configuration as the application. With no
arguments it prints the missing count; `--apply` processes that snapshot one file
at a time. It skips running creations, verifies original checksums and locks each
creation against concurrent migration/deletion. Rerunning safely skips completed
thumbnails. Failed derivatives can also be retried by the authenticated thumbnail
endpoint without regenerating the original.
