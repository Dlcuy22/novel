// File: novel_wayland.c
// Purpose: Native C helper library implementing Wayland graphics interface
// for Novel standard library. Handles connection, shared memory allocations,
// xdg-shell window mapping, event dispatching, and simple text drawing.
//
// Key Components:
//   - wayland_open: connects to server and maps window
//   - wayland_clear: clears pixels buffer with color
//   - wayland_draw_text: renders text using 8x8 font
//   - wayland_present: commits pixel buffer to display
//   - wayland_dispatch: polls display fd and dispatches events non-blockingly
//   - wayland_close: releases resources and disconnects
//
// Dependencies:
//   - wayland-client: Wayland client core API
//   - xdg-shell.h: XDG shell extension client bindings
//   - font8x8_basic.h: Basic latin monochrome bitmap font

#define _GNU_SOURCE
#include <sys/mman.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <unistd.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <poll.h>
#include <wayland-client.h>
#include "xdg-shell.h"
#include "font8x8_basic.h"

struct WaylandWindow {
    struct wl_display *display;
    struct wl_registry *registry;
    struct wl_compositor *compositor;
    struct wl_shm *shm;
    struct xdg_wm_base *xdg_wm_base;
    
    struct wl_surface *surface;
    struct xdg_surface *xdg_surface;
    struct xdg_toplevel *xdg_toplevel;
    
    int width;
    int height;
    int stride;
    int size;
    int fd;
    
    struct wl_shm_pool *pool;
    struct wl_buffer *buffer;
    uint32_t *data;
    
    int configured;
    int running;
};

/*
create_shm_file creates an anonymous shared memory file descriptor.

    params:
          size: bytes to allocate
    returns:
          int: file descriptor or -1 on error
*/
static int create_shm_file(off_t size) {
    int fd = memfd_create("novel-wayland-shm", MFD_CLOEXEC);
    if (fd < 0) {
        char name[] = "/dev/shm/novel-wayland-shm-XXXXXX";
        fd = mkstemp(name);
        if (fd >= 0) {
            unlink(name);
        }
    }
    if (fd >= 0) {
        if (ftruncate(fd, size) < 0) {
            close(fd);
            return -1;
        }
    }
    return fd;
}

static void xdg_surface_configure(void *data, struct xdg_surface *xdg_surface, uint32_t serial) {
    struct WaylandWindow *win = data;
    xdg_surface_ack_configure(xdg_surface, serial);
    win->configured = 1;
}

static const struct xdg_surface_listener xdg_surface_listener = {
    .configure = xdg_surface_configure,
};

static void xdg_toplevel_configure(void *data, struct xdg_toplevel *xdg_toplevel, int32_t width, int32_t height, struct wl_array *states) {
}

static void xdg_toplevel_close(void *data, struct xdg_toplevel *xdg_toplevel) {
    struct WaylandWindow *win = data;
    win->running = 0;
}

static const struct xdg_toplevel_listener xdg_toplevel_listener = {
    .configure = xdg_toplevel_configure,
    .close = xdg_toplevel_close,
};

static void xdg_wm_base_ping(void *data, struct xdg_wm_base *xdg_wm_base, uint32_t serial) {
    xdg_wm_base_pong(xdg_wm_base, serial);
}

static const struct xdg_wm_base_listener xdg_wm_base_listener = {
    .ping = xdg_wm_base_ping,
};

static void registry_global(void *data, struct wl_registry *registry, uint32_t name, const char *interface, uint32_t version) {
    struct WaylandWindow *win = data;
    if (strcmp(interface, wl_compositor_interface.name) == 0) {
        win->compositor = wl_registry_bind(registry, name, &wl_compositor_interface, 1);
    } else if (strcmp(interface, wl_shm_interface.name) == 0) {
        win->shm = wl_registry_bind(registry, name, &wl_shm_interface, 1);
    } else if (strcmp(interface, xdg_wm_base_interface.name) == 0) {
        win->xdg_wm_base = wl_registry_bind(registry, name, &xdg_wm_base_interface, 1);
        xdg_wm_base_add_listener(win->xdg_wm_base, &xdg_wm_base_listener, win);
    }
}

static void registry_global_remove(void *data, struct wl_registry *registry, uint32_t name) {
}

static const struct wl_registry_listener registry_listener = {
    .global = registry_global,
    .global_remove = registry_global_remove,
};

/*
wayland_close destroys all window and display objects and releases memory.

    params:
          win: window pointer to close
*/
void wayland_close(struct WaylandWindow* win) {
    if (!win) return;
    
    if (win->data && win->data != MAP_FAILED) {
        munmap(win->data, win->size);
    }
    if (win->fd >= 0) {
        close(win->fd);
    }
    if (win->buffer) {
        wl_buffer_destroy(win->buffer);
    }
    if (win->pool) {
        wl_shm_pool_destroy(win->pool);
    }
    if (win->xdg_toplevel) {
        xdg_toplevel_destroy(win->xdg_toplevel);
    }
    if (win->xdg_surface) {
        xdg_surface_destroy(win->xdg_surface);
    }
    if (win->surface) {
        wl_surface_destroy(win->surface);
    }
    if (win->xdg_wm_base) {
        xdg_wm_base_destroy(win->xdg_wm_base);
    }
    if (win->shm) {
        wl_proxy_destroy((struct wl_proxy *)win->shm);
    }
    if (win->compositor) {
        wl_proxy_destroy((struct wl_proxy *)win->compositor);
    }
    if (win->registry) {
        wl_registry_destroy(win->registry);
    }
    if (win->display) {
        wl_display_disconnect(win->display);
    }
    free(win);
}

/*
wayland_open establishes display connection, maps surfaces, and prepares shared buffers.

    params:
          title: window title
          width: window width in pixels
          height: window height in pixels
    returns:
          struct WaylandWindow*: window state pointer or NULL on failure
*/
struct WaylandWindow* wayland_open(const char* title, int width, int height) {
    struct WaylandWindow *win = calloc(1, sizeof(struct WaylandWindow));
    if (!win) return NULL;
    
    win->width = width;
    win->height = height;
    win->stride = width * 4;
    win->size = win->stride * height;
    win->running = 1;
    win->fd = -1;
    
    win->display = wl_display_connect(NULL);
    if (!win->display) {
        free(win);
        return NULL;
    }
    
    win->registry = wl_display_get_registry(win->display);
    wl_registry_add_listener(win->registry, &registry_listener, win);
    
    wl_display_roundtrip(win->display);
    wl_display_roundtrip(win->display);
    
    if (!win->compositor || !win->shm || !win->xdg_wm_base) {
        wayland_close(win);
        return NULL;
    }
    
    win->surface = wl_compositor_create_surface(win->compositor);
    win->xdg_surface = xdg_wm_base_get_xdg_surface(win->xdg_wm_base, win->surface);
    xdg_surface_add_listener(win->xdg_surface, &xdg_surface_listener, win);
    
    win->xdg_toplevel = xdg_surface_get_toplevel(win->xdg_surface);
    xdg_toplevel_add_listener(win->xdg_toplevel, &xdg_toplevel_listener, win);
    xdg_toplevel_set_title(win->xdg_toplevel, title);
    
    wl_surface_commit(win->surface);
    
    wl_display_roundtrip(win->display);
    
    win->fd = create_shm_file(win->size);
    if (win->fd < 0) {
        wayland_close(win);
        return NULL;
    }
    
    win->data = mmap(NULL, win->size, PROT_READ | PROT_WRITE, MAP_SHARED, win->fd, 0);
    if (win->data == MAP_FAILED) {
        wayland_close(win);
        return NULL;
    }
    
    win->pool = wl_shm_create_pool(win->shm, win->fd, win->size);
    win->buffer = wl_shm_pool_create_buffer(win->pool, 0, win->width, win->height, win->stride, WL_SHM_FORMAT_XRGB8888);
    
    memset(win->data, 0, win->size);
    
    return win;
}

/*
wayland_clear paints the window buffer with a single solid background color.

    params:
          win: window pointer
          color: hex representation of color (XRGB8888)
*/
void wayland_clear(struct WaylandWindow* win, uint32_t color) {
    if (!win || !win->data) return;
    for (int i = 0; i < win->width * win->height; i++) {
        win->data[i] = color;
    }
}

/*
wayland_draw_text renders a text string on the window pixel buffer.

    params:
          win: window pointer
          x: x-coordinate on screen
          y: y-coordinate on screen
          text: text content to render
          color: color of the text (XRGB8888)
*/
void wayland_draw_text(struct WaylandWindow* win, int x, int y, const char* text, uint32_t color) {
    if (!win || !win->data || !text) return;
    
    int start_x = x;
    while (*text) {
        char c = *text;
        if (c == '\n') {
            y += 10;
            x = start_x;
        } else if (c >= 32 && c < 128) {
            for (int row = 0; row < 8; row++) {
                uint8_t byte = font8x8_basic[(int)c][row];
                for (int col = 0; col < 8; col++) {
                    if (byte & (1 << col)) {
                        int px = x + col;
                        int py = y + row;
                        if (px >= 0 && px < win->width && py >= 0 && py < win->height) {
                            win->data[py * win->width + px] = color;
                        }
                    }
                }
            }
            x += 8;
        } else if (c == ' ') {
            x += 8;
        }
        text++;
    }
}

/*
wayland_present commits the memory buffer damage and triggers redrawing on compositor side.

    params:
          win: window pointer
*/
void wayland_present(struct WaylandWindow* win) {
    if (!win || !win->surface || !win->buffer) return;
    wl_surface_attach(win->surface, win->buffer, 0, 0);
    wl_surface_damage(win->surface, 0, 0, win->width, win->height);
    wl_surface_commit(win->surface);
    wl_display_flush(win->display);
}

/*
wayland_dispatch handles non-blocking events polling the Wayland display socket.

    params:
          win: window pointer
    returns:
          int: 0 if still running, -1 if window was closed or connection lost
*/
int wayland_dispatch(struct WaylandWindow* win) {
    if (!win || !win->display || !win->running) return -1;
    
    struct pollfd pfd = {
        .fd = wl_display_get_fd(win->display),
        .events = POLLIN,
    };
    
    int ret = poll(&pfd, 1, 0);
    if (ret > 0) {
        if (wl_display_dispatch(win->display) < 0) {
            win->running = 0;
            return -1;
        }
    } else if (ret < 0) {
        win->running = 0;
        return -1;
    } else {
        wl_display_dispatch_pending(win->display);
    }
    wl_display_flush(win->display);
    return win->running ? 0 : -1;
}
