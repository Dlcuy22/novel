/* novel_image.c: a flat C shim over stb_image / stb_image_write.
 *
 * Purpose:
 *   Exposes a small, FFI-friendly C API the Novel std/image module loads via
 *   LuaJIT's ffi. stb does its own error handling (no setjmp), so wrapping it
 *   behind plain functions is safe across the FFI boundary, unlike libjpeg /
 *   libpng. Pixels are always returned as tightly-packed 8-bit RGBA so the Lua
 *   side has one predictable layout.
 *
 * Build:
 *   See the Makefile `image-lib` target. Produces libnovel_image.{so,dylib}
 *   placed in the runtime directory so the FFI find it on LUA_PATH's dir.
 *
 * Memory:
 *   novel_image_load returns a buffer owned by stb; the caller MUST release it
 *   with novel_image_free. Encoded output from the *_to_mem functions is
 *   malloc'd here and freed with novel_image_free_mem.
 */

#include <stdlib.h>
#include <string.h>

#define STB_IMAGE_IMPLEMENTATION
#include "vendor/stb_image.h"

#define STB_IMAGE_WRITE_IMPLEMENTATION
#include "vendor/stb_image_write.h"

/* Collects encoded bytes from stb_image_write's callback into a growable buf. */
typedef struct {
  unsigned char *data;
  int len;
  int cap;
} mem_sink;

static void sink_write(void *ctx, void *data, int size) {
  mem_sink *s = (mem_sink *)ctx;
  if (s->len + size > s->cap) {
    int ncap = s->cap * 2;
    if (ncap < s->len + size) ncap = s->len + size;
    unsigned char *nd = (unsigned char *)realloc(s->data, ncap);
    if (!nd) return;            /* out of memory: drop the write */
    s->data = nd;
    s->cap = ncap;
  }
  memcpy(s->data + s->len, data, size);
  s->len += size;
}

/* novel_image_load decodes an image file (PNG/JPEG/BMP/TGA/GIF/...) into a
 * freshly allocated RGBA8 buffer. On success returns the pixel pointer and
 * writes width/height/source-channel-count through the out params. On failure
 * returns NULL (and *w/*h are 0). Free the result with novel_image_free. */
unsigned char *novel_image_load(const char *path, int *w, int *h, int *channels) {
  int n = 0;
  unsigned char *pixels = stbi_load(path, w, h, &n, 4); /* force 4 = RGBA */
  if (channels) *channels = n;
  return pixels; /* NULL on failure */
}

/* novel_image_load_mem is like novel_image_load but decodes from a memory
 * buffer (e.g. an HTTP body) rather than a path. */
unsigned char *novel_image_load_mem(const unsigned char *buf, int len,
                                    int *w, int *h, int *channels) {
  int n = 0;
  unsigned char *pixels = stbi_load_from_memory(buf, len, w, h, &n, 4);
  if (channels) *channels = n;
  return pixels;
}

/* novel_image_failure returns stb's last decode error message (static). */
const char *novel_image_failure(void) {
  return stbi_failure_reason();
}

/* novel_image_free releases a buffer returned by the load functions. */
void novel_image_free(unsigned char *pixels) {
  stbi_image_free(pixels);
}

/* Encode RGBA8 pixels to PNG/JPEG in memory. Returns a malloc'd buffer (set
 * *out_len) the caller frees with novel_image_free_mem, or NULL on failure. */
unsigned char *novel_image_encode_png(const unsigned char *pixels,
                                      int w, int h, int *out_len) {
  mem_sink s = { NULL, 0, 0 };
  int ok = stbi_write_png_to_func(sink_write, &s, w, h, 4, pixels, w * 4);
  if (!ok) { free(s.data); return NULL; }
  if (out_len) *out_len = s.len;
  return s.data;
}

unsigned char *novel_image_encode_jpg(const unsigned char *pixels,
                                      int w, int h, int quality, int *out_len) {
  mem_sink s = { NULL, 0, 0 };
  if (quality < 1) quality = 1;
  if (quality > 100) quality = 100;
  int ok = stbi_write_jpg_to_func(sink_write, &s, w, h, 4, pixels, quality);
  if (!ok) { free(s.data); return NULL; }
  if (out_len) *out_len = s.len;
  return s.data;
}

/* novel_image_free_mem releases an encoded buffer from the encode functions. */
void novel_image_free_mem(unsigned char *buf) {
  free(buf);
}
