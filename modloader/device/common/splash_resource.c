#include "splash_resource.h"

typedef unsigned int u32;
typedef unsigned long long u64;

extern int open(const char *path, int flags, ...);
extern int close(int fd);
extern int read(int fd, void *buf, unsigned long count);
extern int write(int fd, const void *buf, unsigned long count);
extern void *malloc(unsigned long size);
extern void free(void *ptr);
extern int mprotect(void *addr, unsigned long len, int prot);
extern void *mmap(void *addr, unsigned long len, int prot, int flags, int fd, long offset);
extern int munmap(void *addr, unsigned long len);
extern int snprintf(char *dst, unsigned long cap, const char *fmt, ...);

#define SPLASH_LOG "/data/prime-unlimited/logs/ui-hook.log"
#define SPLASH_FILE "/data/prime-unlimited/ui/splash/SplashScreen.png"
#define SPLASH_MAX (2u * 1024u * 1024u)
#define SPLASH_PAGE 4096u
#define SPLASH_COMPRESSED 0x01u
#define SPLASH_DIRECTORY 0x02u
#define SPLASH_ZSTD 0x04u
#define SPLASH_MAP_PRIVATE 0x02
#define SPLASH_MAP_ANONYMOUS 0x20
#define SPLASH_MAP_FIXED_NOREPLACE 0x100000

#ifdef SPLASH_HOST_TEST
const char *splash_override_file;
#endif

static u32 be32(const unsigned char *p) {
    return ((u32)p[0] << 24) | ((u32)p[1] << 16) | ((u32)p[2] << 8) | p[3];
}

static void put32(unsigned char *p, u32 v) {
    p[0] = (unsigned char)(v >> 24);
    p[1] = (unsigned char)(v >> 16);
    p[2] = (unsigned char)(v >> 8);
    p[3] = (unsigned char)v;
}

static void logline(const char *text) {
    int fd = open(SPLASH_LOG, 1 | 64 | 1024, 0644);
    unsigned n = 0;
    if (fd < 0)
        return;
    while (text[n])
        n++;
    write(fd, text, n);
    write(fd, "\n", 1);
    close(fd);
}

static int protect(void *addr, u32 bytes) {
    unsigned long start = (unsigned long)addr & ~(unsigned long)(SPLASH_PAGE - 1);
    unsigned long end = ((unsigned long)addr + bytes + (SPLASH_PAGE - 1)) & ~(unsigned long)(SPLASH_PAGE - 1);
    if (end <= start)
        return 0;
    return mprotect((void *)start, (unsigned)(end - start), 3) == 0;
}

/* 1577836800000 ms == 367 * 2^32 + 1583802368, plus a 31-bit content hash.
 * The sum of the low halves never crosses 2^32, so no 64-bit division is required. */
static void put_stamp(unsigned char *node, const unsigned char *bytes, unsigned len) {
    u32 hash = 2166136261u;
    u32 extra;
    u32 lo;
    unsigned i;
    for (i = 0; i < len; i++)
        hash = (hash ^ bytes[i]) * 16777619u;
    extra = hash & 0x7fffffffu;
    lo = 1583802368u + extra;
    put32(node + 14, 367u);
    put32(node + 18, lo);
}

static void copy_bytes(unsigned char *dst, const unsigned char *src, unsigned len) {
    unsigned i;
    for (i = 0; i < len; i++)
        dst[i] = src[i];
}

static int same_text(const char *left, const char *right) {
    while (*left && *right && *left == *right) {
        left++;
        right++;
    }
    return *left == *right;
}

static int ends_with(const char *path, const char *suffix) {
    unsigned path_len = 0;
    unsigned suffix_len = 0;
    unsigned i;
    while (path[path_len])
        path_len++;
    while (suffix[suffix_len])
        suffix_len++;
    if (path_len < suffix_len)
        return 0;
    for (i = 0; i < suffix_len; i++) {
        if (path[path_len - suffix_len + i] != suffix[i])
            return 0;
    }
    return 1;
}

int splash_path_is_second_stage(const char *path) {
    /* Engine's 1280x800 start screen. The 1366x768 _16x9 asset does not match. */
    return same_text(path, "img/SplashScreen.png") || ends_with(path, "/img/SplashScreen.png");
}

int splash_payload_accepted(const unsigned char *png, unsigned png_len) {
    static const unsigned char sig[8] = {137, 80, 78, 71, 13, 10, 26, 10};
    unsigned i;
    if (png_len < 24 || png_len > SPLASH_MAX)
        return 0;
    for (i = 0; i < 8; i++) {
        if (png[i] != sig[i])
            return 0;
    }
    if (png[12] != 'I' || png[13] != 'H' || png[14] != 'D' || png[15] != 'R')
        return 0;
    return be32(png + 16) == 1280u && be32(png + 20) == 800u;
}

int splash_relocation_allowed(u64 blob, u64 data, int require_sxtw) {
    u64 diff;
    if (!require_sxtw)
        return 1;
    if (blob < data)
        return 0;
    diff = blob - data;
    return diff <= 0x7fffffffull;
}

unsigned splash_map_span(unsigned bytes) {
    return (bytes + (SPLASH_PAGE - 1)) & ~(SPLASH_PAGE - 1);
}

static int node_bytes(int version) {
    return version >= 2 ? 22 : 14;
}

int splash_commit_inplace(unsigned char *node, int version, unsigned char *slot,
    unsigned slot_cap, const unsigned char *png, unsigned png_len) {
    u32 flags;
    if (png_len == 0 || png_len > slot_cap)
        return 0;
    flags = ((u32)node[4] << 8) | node[5];
    if (flags & (SPLASH_COMPRESSED | SPLASH_ZSTD | SPLASH_DIRECTORY))
        return 0;
    if (!protect(slot, 4u + slot_cap) || !protect(node, (u32)node_bytes(version)))
        return 0;
    put32(slot, png_len);
    copy_bytes(slot + 4, png, png_len);
    if (version >= 2)
        put_stamp(node, png, png_len);
    return 1;
}

int splash_commit_relocated(unsigned char *node, int version, const unsigned char *data,
    unsigned char *blob, const unsigned char *png, unsigned png_len, int require_sxtw) {
    u64 blob_addr = (u64)(unsigned long)blob;
    u64 data_addr = (u64)(unsigned long)data;
    u32 offset;
    if (png_len == 0 || png_len > SPLASH_MAX || !blob)
        return 0;
    if (!splash_relocation_allowed(blob_addr, data_addr, require_sxtw))
        return 0;
    offset = (u32)(blob_addr - data_addr);
    if (!protect(node, (u32)node_bytes(version)))
        return 0;
    put32(blob, png_len);
    copy_bytes(blob + 4, png, png_len);
    put32(node + 10, offset);
    node[5] = (unsigned char)(node[5] & (unsigned char)~(SPLASH_COMPRESSED | SPLASH_ZSTD));
    if (version >= 2)
        put_stamp(node, png, png_len);
    return 1;
}

static int hex_digit(char c) {
    if (c >= '0' && c <= '9')
        return c - '0';
    if (c >= 'a' && c <= 'f')
        return c - 'a' + 10;
    if (c >= 'A' && c <= 'F')
        return c - 'A' + 10;
    return -1;
}

static int parse_hex(const char *text, unsigned long *out, const char **end) {
    unsigned long value = 0;
    int digits = 0;
    int digit;
    while ((digit = hex_digit(*text)) >= 0) {
        value = (value << 4) | (unsigned)digit;
        text++;
        digits++;
        if (digits > (int)(sizeof(unsigned long) * 2))
            return 0;
    }
    if (!digits)
        return 0;
    *out = value;
    *end = text;
    return 1;
}

unsigned char *splash_map_near(const unsigned char *data, unsigned bytes) {
#if !defined(__aarch64__)
    (void)data;
    (void)bytes;
    return 0;
#else
    unsigned long base = (unsigned long)data;
    unsigned long span = splash_map_span(bytes);
    unsigned long lo;
    unsigned long hi;
    unsigned long cursor;
    char *maps;
    unsigned got = 0;
    int fd;
    int reading = 1;
    if (bytes == 0 || span < bytes || base > ~0ul - 0x20000000ul)
        return 0;
    lo = (base + 0x20000000ul + (SPLASH_PAGE - 1)) & ~(unsigned long)(SPLASH_PAGE - 1);
    hi = base + 0x7ffffffful - span;
    if (lo > hi)
        return 0;
    maps = malloc(1024u * 1024u);
    if (!maps)
        return 0;
    fd = open("/proc/self/maps", 0);
    if (fd >= 0) {
        while (got < 1024u * 1024u - 1 && reading) {
            int n = read(fd, maps + got, 1024u * 1024u - 1 - got);
            if (n < 0) {
                got = 0;
                break;
            }
            if (n == 0)
                reading = 0;
            else
                got += (unsigned)n;
        }
        close(fd);
    }
    maps[got] = 0;
    cursor = lo;
    if (got) {
        char *line = maps;
        while (*line && cursor <= hi) {
            unsigned long start = 0;
            unsigned long end = 0;
            const char *rest = line;
            char *next = line;
            while (*next && *next != '\n')
                next++;
            if (*next == '\n')
                *next++ = 0;
            else
                break;
            if (parse_hex(line, &start, &rest) && *rest == '-' && parse_hex(rest + 1, &end, &rest)) {
                if (end > cursor && start < cursor + span) {
                    cursor = (end + (SPLASH_PAGE - 1)) & ~(unsigned long)(SPLASH_PAGE - 1);
                } else if (start >= cursor + span && start > cursor) {
                    break;
                }
            }
            line = next;
        }
    }
    free(maps);
    if (cursor <= hi) {
        void *mapped = mmap((void *)cursor, (unsigned)span, 3,
            SPLASH_MAP_PRIVATE | SPLASH_MAP_ANONYMOUS | SPLASH_MAP_FIXED_NOREPLACE, -1, 0);
        if (mapped == (void *)cursor)
            return (unsigned char *)mapped;
        if (mapped != (void *)-1)
            munmap(mapped, (unsigned)span);
    }
    return 0;
#endif
}

static int read_replacement(unsigned char **out, unsigned *out_len) {
    unsigned char *buf;
    unsigned got = 0;
#ifdef SPLASH_HOST_TEST
    const char *path = splash_override_file ? splash_override_file : SPLASH_FILE;
#else
    const char *path = SPLASH_FILE;
#endif
    int fd = open(path, 0);
    if (fd < 0)
        return 0;
    buf = malloc(SPLASH_MAX + 1);
    if (!buf) {
        close(fd);
        return -1;
    }
    while (got < SPLASH_MAX + 1) {
        int n = read(fd, buf + got, SPLASH_MAX + 1 - got);
        if (n < 0) {
            free(buf);
            close(fd);
            return -1;
        }
        if (n == 0)
            break;
        got += (unsigned)n;
    }
    close(fd);
    *out = buf;
    *out_len = got;
    return 1;
}

static int apply_png(unsigned char *node, int version, unsigned char *data,
    const unsigned char *png, unsigned png_len) {
    u32 flags = ((u32)node[4] << 8) | node[5];
    u32 slot_off = be32(node + 10);
    u32 slot_cap = be32(data + slot_off);
    unsigned char *blob;
    if (!splash_payload_accepted(png, png_len))
        return 3;
    if ((flags & (SPLASH_COMPRESSED | SPLASH_ZSTD | SPLASH_DIRECTORY)) == 0 && png_len <= slot_cap &&
        splash_commit_inplace(node, version, data + slot_off, slot_cap, png, png_len))
        return 1;
#if defined(__aarch64__)
    blob = splash_map_near(data, 4u + png_len);
    if (!blob)
        return 4;
    if (!splash_commit_relocated(node, version, data, blob, png, png_len, 1)) {
        munmap(blob, splash_map_span(4u + png_len));
        return 4;
    }
    return 2;
#else
    blob = malloc(4u + png_len);
    if (!blob)
        return 4;
    if (!splash_commit_relocated(node, version, data, blob, png, png_len, 0)) {
        free(blob);
        return 4;
    }
    return 2;
#endif
}

static void append_name(char *dest, const char *parent, const char *name) {
    unsigned n = 0;
    unsigned i = 0;
    while (parent[n] && n < 500) {
        dest[n] = parent[n];
        n++;
    }
    if (name[0] == 0) {
        dest[n] = 0;
        return;
    }
    if (n && n < 510)
        dest[n++] = '/';
    while (name[i] && n < 510)
        dest[n++] = name[i++];
    dest[n] = 0;
}

static void walk(int version, unsigned char *tree, const unsigned char *names, unsigned char *data,
    u32 index, int depth, const char *path) {
    u32 node_size = version >= 2 ? 22u : 14u;
    unsigned char *node;
    u32 name_off;
    u32 flags;
    const unsigned char *name_ptr;
    u32 name_len;
    u32 i;
    char name[192];
    char next[512];
    if (depth > 40)
        return;
    node = tree + index * node_size;
    name_off = be32(node);
    flags = ((u32)node[4] << 8) | node[5];
    name_ptr = names + name_off;
    name_len = ((u32)name_ptr[0] << 8) | name_ptr[1];
    name_ptr += 6;
    if (name_len > 191)
        name_len = 191;
    for (i = 0; i < name_len; i++)
        name[i] = (char)name_ptr[i * 2 + 1];
    name[name_len] = 0;
    append_name(next, path, name);
    if (flags & SPLASH_DIRECTORY) {
        u32 count = be32(node + 6);
        u32 first = be32(node + 10);
        for (i = 0; i < count; i++)
            walk(version, tree, names, data, first + i, depth + 1, next);
        return;
    }
    if (!splash_path_is_second_stage(next))
        return;
#ifdef DIAGNOSTIC_ONLY
    {
        char message[160];
        u32 slot_off = be32(node + 10);
        u32 slot_cap = be32(data + slot_off);
        snprintf(message, sizeof message,
            "ui-hook: splash candidate flags=%u len=%u, diagnostic build keeps stock splash",
            flags, slot_cap);
        logline(message);
    }
#else
    {
        unsigned char *png = 0;
        unsigned png_len = 0;
        int read_rc = read_replacement(&png, &png_len);
        int applied;
        if (read_rc == 0) {
            logline("ui-hook: splash file absent, stock splash kept");
            return;
        }
        if (read_rc < 0) {
            logline("ui-hook: splash payload rejected, stock splash kept");
            return;
        }
        applied = apply_png(node, version, data, png, png_len);
        free(png);
        if (applied == 1)
            logline("ui-hook: splash swap applied SplashScreen.png inplace");
        else if (applied == 2)
            logline("ui-hook: splash swap applied SplashScreen.png relocated");
        else if (applied == 3)
            logline("ui-hook: splash payload rejected, stock splash kept");
        else
            logline("ui-hook: splash relocate failed, stock splash kept");
    }
#endif
}

void splash_visit(int version, unsigned char *tree, const unsigned char *names, unsigned char *data) {
    if (version < 1 || version > 3 || !tree || !names || !data)
        return;
    walk(version, tree, names, data, 0, 0, "");
}
