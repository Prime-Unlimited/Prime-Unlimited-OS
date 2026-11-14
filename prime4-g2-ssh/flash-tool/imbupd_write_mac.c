/*
 * imbupd_write_mac.c — macOS Flash-Tool für Denon Prime 4 G2 (JP24)
 *
 * Spricht das proprietäre imbupd-Protokoll über USB Bulk-Transfers
 * mit dem Gerät im Firmware Update Mode (VID 0x15E4, PID 0xD014).
 *
 * Protokoll reverse-engineered aus der ARM64-Assembly der Denon
 * Firmware Updater macOS-App (November 2026).
 *
 * Build:
 *   brew install libusb
 *   cc -o imbupd_write_mac imbupd_write_mac.c \
 *     -I/opt/homebrew/include -L/opt/homebrew/lib -lusb-1.0
 *
 * Usage:
 *   sudo ./imbupd_write_mac query
 *   sudo ./imbupd_write_mac write data /path/to/image.img
 *   sudo ./imbupd_write_mac reboot
 *
 * MIT License — DJ MR. BROWN
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <errno.h>
#include <libusb-1.0/libusb.h>

/* USB device identifiers */
#define VID          0x15E4
#define PID          0xD014
#define EP_OUT       0x01
#define EP_IN        0x81

/* imbupd protocol constants */
#define IMBUPD_MAGIC_REQ   0x4F305A41  /* "AZ0O" — Out/Request */
#define IMBUPD_MAGIC_RESP  "AZ0I"     /* Raw response bytes, independent of host endianness */

/* Opcodes (from ARM64 assembly of Denon Firmware Updater) */
#define OP_GET_PUBKEY    0x5AA5F906
#define OP_WRITE_BOOT    0x5AA5FA05
#define OP_WRITE_PART    0x5AA5FB04
#define OP_ERASE_PART    0x5AA5FC03
#define OP_GET_PROPERTY  0x5AA5FD02
#define OP_REBOOT        0x5AA5FE01

/* Transfer parameters */
#define CHUNK_SIZE       0x100000  /* 1 MiB per write chunk */
#define TIMEOUT_SHORT    5000     /* 5s for queries */
#define TIMEOUT_LONG     300000   /* 5min for flash operations */
#define MAX_PKT          4096

/* Response status at offset 0x14 */
#define STATUS_OK        0x01
#define STATUS_ERROR     0x03

/* Reboot payload magic */
#define REBOOT_MAGIC     0xA55A0101
#define WRITE_CONFIRMATION "FLASH JP24 /data"

static libusb_device_handle *g_dev;

/*
 * imbupd packet header (0x18 bytes):
 *   0x00  uint32 LE  Magic (IMBUPD_MAGIC_REQ or IMBUPD_MAGIC_RESP)
 *   0x04  uint32 LE  Opcode
 *   0x08  uint64 LE  Reserved (0, or total size for write-start)
 *   0x10  uint16 LE  Sequence number
 *   0x12  uint16 LE  Padding (0)
 *   0x14  uint16 LE  Padding (0) / Status in response
 *   0x16  uint16 LE  Packet size (total including header)
 *   0x18  ...        Payload
 */

static int usb_write(const void *buf, int len, int timeout) {
    int transferred = 0;
    int r = libusb_bulk_transfer(g_dev, EP_OUT, (unsigned char *)buf,
                                  len, &transferred, timeout);
    if (r != 0) {
        fprintf(stderr, "USB write error: %s\n", libusb_strerror(r));
        return -1;
    }
    return transferred;
}

static int usb_read(void *buf, int len, int timeout) {
    int transferred = 0;
    int r = libusb_bulk_transfer(g_dev, EP_IN, (unsigned char *)buf,
                                  len, &transferred, timeout);
    if (r != 0) {
        fprintf(stderr, "USB read error: %s\n", libusb_strerror(r));
        return -1;
    }
    return transferred;
}

static void build_header(unsigned char *pkt, uint32_t opcode,
                          uint16_t seq, uint16_t pkt_size) {
    memset(pkt, 0, 0x18);
    uint32_t magic = IMBUPD_MAGIC_REQ;
    memcpy(pkt + 0x00, &magic, 4);
    memcpy(pkt + 0x04, &opcode, 4);
    memcpy(pkt + 0x10, &seq, 2);
    memcpy(pkt + 0x16, &pkt_size, 2);
}

static int send_recv(unsigned char *pkt, uint16_t pkt_size,
                     unsigned char *resp, int resp_size, int timeout) {
    if (usb_write(pkt, pkt_size, timeout) < 0)
        return -1;
    int n = usb_read(resp, resp_size, timeout);
    if (n < 0x18) {
        fprintf(stderr, "Short response (%d bytes)\n", n);
        return -1;
    }
    if (memcmp(resp, IMBUPD_MAGIC_RESP, 4) != 0) {
        fprintf(stderr, "Bad response magic: %02x %02x %02x %02x (expected AZ0I)\n",
                resp[0], resp[1], resp[2], resp[3]);
        return -1;
    }
    return n;
}

/* Query a device property (e.g. "version", "power-source") */
static int get_property(const char *name, char *out, int out_size) {
    unsigned char pkt[MAX_PKT];
    unsigned char resp[MAX_PKT];
    int name_len = strlen(name) + 1;
    uint16_t pkt_size = 0x18 + name_len;
    static uint16_t seq = 0;

    build_header(pkt, OP_GET_PROPERTY, seq++, pkt_size);
    memcpy(pkt + 0x18, name, name_len);

    int n = send_recv(pkt, pkt_size, resp, sizeof(resp), TIMEOUT_SHORT);
    if (n < 0) return -1;

    uint16_t status;
    memcpy(&status, resp + 0x14, 2);
    if (status == STATUS_ERROR) {
        fprintf(stderr, "Property '%s': not found\n", name);
        return -1;
    }

    int payload_len = n - 0x18;
    if (payload_len <= 0) {
        out[0] = 0;
        return 0;
    }
    if (payload_len >= out_size)
        payload_len = out_size - 1;
    memcpy(out, resp + 0x18, payload_len);
    out[payload_len] = 0;
    return 0;
}

/* Erase a partition by name (e.g. "data") */
static int erase_part(const char *part_name) {
    unsigned char pkt[MAX_PKT];
    unsigned char resp[MAX_PKT];
    int name_len = strlen(part_name) + 1;
    uint16_t pkt_size = 0x18 + name_len;
    static uint16_t seq = 100;

    printf("Erasing partition '%s'...\n", part_name);

    build_header(pkt, OP_ERASE_PART, seq++, pkt_size);
    memcpy(pkt + 0x18, part_name, name_len);

    int n = send_recv(pkt, pkt_size, resp, sizeof(resp), TIMEOUT_LONG);
    if (n < 0) return -1;

    uint16_t status;
    memcpy(&status, resp + 0x14, 2);
    if (status == STATUS_ERROR) {
        fprintf(stderr, "Erase failed for '%s'\n", part_name);
        return -1;
    }

    printf("Erase complete.\n");
    return 0;
}

/* Write an image file to a partition */
static int write_part(const char *part_name, const char *image_path) {
    int fd = open(image_path, O_RDONLY);
    if (fd < 0) {
        perror(image_path);
        return -1;
    }

    off_t file_size = lseek(fd, 0, SEEK_END);
    lseek(fd, 0, SEEK_SET);

    if (file_size <= 0) {
        fprintf(stderr, "Empty image file\n");
        close(fd);
        return -1;
    }

    printf("Writing '%s' to partition '%s' (%lld bytes)...\n",
           image_path, part_name, (long long)file_size);

    /* Send write-start header: opcode WRITE_PART, total size at 0x08,
       partition name at 0x28 (header is 0x28 bytes for write-start) */
    unsigned char start_pkt[256];
    memset(start_pkt, 0, sizeof(start_pkt));
    int name_len = strlen(part_name) + 1;
    uint16_t start_size = 0x28 + name_len;
    static uint16_t seq = 200;

    uint32_t magic = IMBUPD_MAGIC_REQ;
    uint32_t opcode = OP_WRITE_PART;
    memcpy(start_pkt + 0x00, &magic, 4);
    memcpy(start_pkt + 0x04, &opcode, 4);
    /* Total image size at offset 0x08 (uint64 LE) */
    uint64_t total = (uint64_t)file_size;
    memcpy(start_pkt + 0x08, &total, 8);
    uint16_t s = seq++;
    memcpy(start_pkt + 0x10, &s, 2);
    memcpy(start_pkt + 0x16, &start_size, 2);
    /* Partition name at offset 0x28 */
    memcpy(start_pkt + 0x28, part_name, name_len);

    unsigned char resp[MAX_PKT];
    if (usb_write(start_pkt, start_size, TIMEOUT_SHORT) < 0) {
        close(fd);
        return -1;
    }
    int n = usb_read(resp, sizeof(resp), TIMEOUT_LONG);
    if (n < 0x18) {
        fprintf(stderr, "No response to write-start\n");
        close(fd);
        return -1;
    }

    /* Stream data in CHUNK_SIZE chunks */
    unsigned char *chunk = malloc(CHUNK_SIZE);
    if (!chunk) { close(fd); return -1; }

    off_t written = 0;
    while (written < file_size) {
        int to_read = CHUNK_SIZE;
        if (file_size - written < to_read)
            to_read = (int)(file_size - written);

        int rd = read(fd, chunk, to_read);
        if (rd <= 0) {
            fprintf(stderr, "Read error at offset %lld\n", (long long)written);
            free(chunk);
            close(fd);
            return -1;
        }

        if (usb_write(chunk, rd, TIMEOUT_LONG) < 0) {
            free(chunk);
            close(fd);
            return -1;
        }

        written += rd;
        int pct = (int)(written * 100 / file_size);
        printf("\r  %lld / %lld bytes (%d%%)", (long long)written,
               (long long)file_size, pct);
        fflush(stdout);
    }
    printf("\n");

    free(chunk);
    close(fd);

    /* Send completion: single null byte */
    unsigned char zero = 0;
    usb_write(&zero, 1, TIMEOUT_SHORT);

    /* Read final status */
    n = usb_read(resp, sizeof(resp), TIMEOUT_LONG);
    if (n >= 0x18) {
        uint16_t status;
        memcpy(&status, resp + 0x14, 2);
        if (status == STATUS_ERROR) {
            fprintf(stderr, "Write failed!\n");
            return -1;
        }
    }

    printf("Write complete.\n");
    return 0;
}

/* Reboot the device */
static int do_reboot(void) {
    unsigned char pkt[MAX_PKT];
    unsigned char resp[MAX_PKT];
    uint16_t pkt_size = 0x18 + 4;
    static uint16_t seq = 300;

    printf("Rebooting device...\n");

    build_header(pkt, OP_REBOOT, seq++, pkt_size);
    uint32_t reboot_magic = REBOOT_MAGIC;
    memcpy(pkt + 0x18, &reboot_magic, 4);

    /* Send reboot — device may not respond before disconnecting */
    usb_write(pkt, pkt_size, TIMEOUT_SHORT);

    /* Try to read response, but don't fail if device already disconnected */
    usb_read(resp, sizeof(resp), TIMEOUT_SHORT);

    printf("Reboot command sent.\n");
    return 0;
}

static void usage(const char *prog) {
    fprintf(stderr, "Usage:\n");
    fprintf(stderr, "  %s query                     — Show device info\n", prog);
    fprintf(stderr, "  %s write data <image> --confirm \"%s\"\n", prog, WRITE_CONFIRMATION);
    fprintf(stderr, "  %s reboot                    — Reboot device\n", prog);
    fprintf(stderr, "\nThe device must be in Firmware Update Mode (USB PID 0xD014).\n");
    fprintf(stderr, "Requires sudo for USB access on macOS.\n");
}

int main(int argc, char **argv) {
    if (argc < 2) {
        usage(argv[0]);
        return 1;
    }

    if (strcmp(argv[1], "query") == 0 || strcmp(argv[1], "reboot") == 0) {
        if (argc != 2) {
            usage(argv[0]);
            return 1;
        }
    } else if (strcmp(argv[1], "write") == 0) {
        if (argc != 6 || strcmp(argv[2], "data") != 0 ||
            strcmp(argv[4], "--confirm") != 0 ||
            strcmp(argv[5], WRITE_CONFIRMATION) != 0) {
            fprintf(stderr,
                    "Refusing to write: only /data is supported and the exact confirmation phrase is required.\n");
            usage(argv[0]);
            return 1;
        }
    } else {
        usage(argv[0]);
        return 1;
    }

    int r = libusb_init(NULL);
    if (r != 0) {
        fprintf(stderr, "libusb_init failed: %s\n", libusb_strerror(r));
        return 1;
    }

    g_dev = libusb_open_device_with_vid_pid(NULL, VID, PID);
    if (!g_dev) {
        fprintf(stderr, "Device not found (VID=%04x PID=%04x).\n", VID, PID);
        fprintf(stderr, "Is the Prime 4 G2 in Firmware Update Mode?\n");
        libusb_exit(NULL);
        return 1;
    }

    libusb_set_auto_detach_kernel_driver(g_dev, 1);

    r = libusb_claim_interface(g_dev, 0);
    if (r != 0) {
        fprintf(stderr, "Cannot claim interface: %s\n", libusb_strerror(r));
        libusb_close(g_dev);
        libusb_exit(NULL);
        return 1;
    }

    int ret = 0;

    if (strcmp(argv[1], "query") == 0) {
        char val[256];
        printf("=== Denon Prime 4 G2 (imbupd) ===\n");
        if (get_property("version", val, sizeof(val)) == 0)
            printf("  version:      %s\n", val);
        if (get_property("power-source", val, sizeof(val)) == 0)
            printf("  power-source: %s\n", val);
        if (get_property("product", val, sizeof(val)) == 0)
            printf("  product:      %s\n", val);
        if (get_property("serial", val, sizeof(val)) == 0)
            printf("  serial:       %s\n", val);

    } else if (strcmp(argv[1], "write") == 0) {
        /* Safety check: device must be on mains power */
        char power[64] = {0};
        if (get_property("power-source", power, sizeof(power)) != 0 ||
            strcmp(power, "mains") != 0) {
            fprintf(stderr, "ERROR: Device is not on mains power ('%s').\n", power);
            fprintf(stderr, "Connect the power cable before flashing!\n");
            ret = 1;
        } else {
            ret = erase_part("data");
            if (ret == 0)
                ret = write_part("data", argv[3]);
        }

    } else if (strcmp(argv[1], "reboot") == 0) {
        ret = do_reboot();

    } else {
        usage(argv[0]);
        ret = 1;
    }

    libusb_release_interface(g_dev, 0);
    libusb_close(g_dev);
    libusb_exit(NULL);
    return ret;
}
