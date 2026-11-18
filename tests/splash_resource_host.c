#include "../modloader/device/common/splash_resource.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <unistd.h>

static int failures = 0;

static void expect(int cond, const char *message) {
    if (cond)
        return;
    fprintf(stderr, "FAIL %s\n", message);
    failures++;
}

static void put16(unsigned char *p, unsigned v) {
    p[0] = (unsigned char)(v >> 8);
    p[1] = (unsigned char)v;
}

static void put32(unsigned char *p, unsigned v) {
    p[0] = (unsigned char)(v >> 24);
    p[1] = (unsigned char)(v >> 16);
    p[2] = (unsigned char)(v >> 8);
    p[3] = (unsigned char)v;
}

static unsigned be32(const unsigned char *p) {
    return ((unsigned)p[0] << 24) | ((unsigned)p[1] << 16) | ((unsigned)p[2] << 8) | p[3];
}

static unsigned add_name(unsigned char *names, unsigned *used, const char *text) {
    unsigned offset = *used;
    unsigned len = (unsigned)strlen(text);
    unsigned i;
    put16(names + offset, len);
    put32(names + offset + 2, 0);
    for (i = 0; i < len; i++) {
        names[offset + 6 + i * 2] = 0;
        names[offset + 6 + i * 2 + 1] = (unsigned char)text[i];
    }
    *used = offset + 6 + len * 2;
    return offset;
}

static void write_dir(unsigned char *node, unsigned name_off, unsigned count, unsigned first) {
    memset(node, 0, 22);
    put32(node, name_off);
    put16(node + 4, 0x0002);
    put32(node + 6, count);
    put32(node + 10, first);
}

static void write_file(unsigned char *node, unsigned name_off, unsigned data_off) {
    memset(node, 0x5a, 22);
    put32(node, name_off);
    put16(node + 4, 0);
    node[6] = 0xab;
    node[7] = 0xcd;
    node[8] = 0x11;
    node[9] = 0x22;
    put32(node + 10, data_off);
}

static unsigned stamp_lo(const unsigned char *bytes, unsigned len) {
    unsigned hash = 2166136261u;
    unsigned i;
    for (i = 0; i < len; i++)
        hash = (hash ^ bytes[i]) * 16777619u;
    return 1583802368u + (hash & 0x7fffffffu);
}

static void test_paths_and_png(void) {
    unsigned char png[32];
    memset(png, 0, sizeof png);
    png[0] = 137;
    png[1] = 80;
    png[2] = 78;
    png[3] = 71;
    png[4] = 13;
    png[5] = 10;
    png[6] = 26;
    png[7] = 10;
    png[12] = 'I';
    png[13] = 'H';
    png[14] = 'D';
    png[15] = 'R';
    put32(png + 16, 1280);
    put32(png + 20, 800);
    expect(splash_path_is_second_stage("Skins/Embedded/img/SplashScreen.png"), "embedded path");
    expect(splash_path_is_second_stage("img/SplashScreen.png"), "short path");
    expect(!splash_path_is_second_stage("Skins/Embedded/img/SplashScreen_16x9.png"), "16x9 excluded");
    expect(!splash_path_is_second_stage("SplashScreen.png"), "bare name excluded");
    expect(splash_payload_accepted(png, sizeof png), "1280x800 png");
    put32(png + 16, 1366);
    expect(!splash_payload_accepted(png, sizeof png), "wrong size rejected");
    png[0] = 0;
    expect(!splash_payload_accepted(png, sizeof png), "bad magic rejected");
}

static void test_stamp_and_inplace(void) {
    unsigned char node[22];
    unsigned char slot[4 + 8];
    unsigned char png[4] = {1, 2, 3, 4};
    memset(node, 0x5a, sizeof node);
    put16(node + 4, 0);
    node[6] = 0xab;
    put32(slot, 8);
    memset(slot + 4, 0x22, 8);
    expect(splash_commit_inplace(node, 2, slot, 8, png, 4), "inplace fits");
    expect(be32(slot) == 4, "inplace length");
    expect(memcmp(slot + 4, png, 4) == 0, "inplace bytes");
    expect(node[6] == 0xab, "country byte preserved");
    expect(be32(node + 14) == 367u, "stamp high half");
    expect(be32(node + 18) == stamp_lo(png, 4), "stamp low half");
    put32(slot, 3);
    expect(!splash_commit_inplace(node, 2, slot, 3, png, 4), "inplace too big");
    put16(node + 4, 0x0001);
    expect(!splash_commit_inplace(node, 2, slot, 8, png, 4), "compressed slot is not inplace");
    expect(node[5] & 0x01, "compressed flag stays when inplace refuses");
}

static void test_relocation_rules(void) {
    unsigned char storage[64];
    unsigned char node[22];
    unsigned char png[4] = {'P', 'N', 'G', '!'};
    unsigned char before[22];
    memset(node, 0, sizeof node);
    node[6] = 0x11;
    node[7] = 0x22;
    node[8] = 0x33;
    node[9] = 0x44;
    put16(node + 4, 0x0005); /* zlib + zstd */
    memcpy(before, node, sizeof node);
    expect(!splash_commit_relocated(node, 2, storage + 32, storage, png, 4, 1), "negative offset refused");
    expect(memcmp(node, before, sizeof node) == 0, "refused relocate does not touch the node");
    expect(splash_commit_relocated(node, 2, storage, storage + 16, png, 4, 1), "positive offset accepted");
    expect(be32(node + 10) == 16u, "relocated offset");
    expect((node[5] & 0x05) == 0, "compression flags cleared");
    expect(node[6] == 0x11 && node[9] == 0x44, "file metadata preserved");
    expect(be32(storage + 16) == 4u, "blob length");
    expect(memcmp(storage + 20, png, 4) == 0, "blob payload");
    expect(splash_relocation_allowed(0x1000, 0x5000, 0), "32-bit wrap is allowed");
    expect(!splash_relocation_allowed(0x1000, 0x5000, 1), "sxtw rejects a backward offset");
    expect(!splash_relocation_allowed(0x90000000ull, 0x1000ull, 1), "sxtw rejects bit 31");
    expect(splash_relocation_allowed(0x20000000ull, 0, 1), "window start is safe");
}

static void test_map_and_real_png(const char *png_path) {
    unsigned char *data = malloc(4096);
    unsigned char *blob;
    unsigned char *file;
    unsigned char node[22];
    FILE *in;
    long size;
    unsigned char *mapped;
    expect(data != NULL, "data buffer");
    memset(data, 0xa5, 4096);
    put32(data, 8);
    blob = splash_map_near(data, 64);
    expect(blob != NULL, "aarch64 window mapping");
    if (blob) {
        expect(splash_relocation_allowed((unsigned long long)(unsigned long)blob,
            (unsigned long long)(unsigned long)data, 1), "mapped blob survives sxtw");
        munmap(blob, splash_map_span(64));
    }
    in = fopen(png_path, "rb");
    expect(in != NULL, "open rendered png");
    if (!in) {
        free(data);
        return;
    }
    fseek(in, 0, SEEK_END);
    size = ftell(in);
    fseek(in, 0, SEEK_SET);
    file = malloc((unsigned)size);
    expect(file && fread(file, 1, (size_t)size, in) == (size_t)size, "read rendered png");
    fclose(in);
    expect(splash_payload_accepted(file, (unsigned)size), "rendered png is 1280x800");
    memset(node, 0, sizeof node);
    node[6] = 0x7e;
    put32(node + 10, 0);
    mapped = splash_map_near(data, 4u + (unsigned)size);
    expect(mapped != NULL, "mapping for the real png");
    if (mapped && file) {
        unsigned char original = data[4];
        expect(splash_commit_relocated(node, 2, data, mapped, file, (unsigned)size, 1), "commit real png");
        expect(be32(node + 14) == 367u, "real png stamp");
        expect(node[6] == 0x7e, "real png keeps country byte");
        expect(data[4] == original, "relocation leaves the stock slot bytes");
        expect(memcmp(mapped + 4, file, (size_t)size) == 0, "mapped bytes match the file");
        munmap(mapped, splash_map_span(4u + (unsigned)size));
    }
    free(file);
    free(data);
}

static void test_absent_file_keeps_stock(void) {
    unsigned char names[256];
    unsigned char tree[22 * 6];
    unsigned char data[32];
    unsigned char tree_before[22 * 6];
    unsigned used = 0;
    unsigned root;
    unsigned skins;
    unsigned embedded;
    unsigned img;
    unsigned splash;
    unsigned wide;
    if (access("/data/prime-unlimited/ui/splash/SplashScreen.png", 0) == 0) {
        fprintf(stderr, "SKIP absent-file visit: replacement PNG is present on this machine\n");
        return;
    }
    memset(names, 0, sizeof names);
    root = add_name(names, &used, "");
    skins = add_name(names, &used, "Skins");
    embedded = add_name(names, &used, "Embedded");
    img = add_name(names, &used, "img");
    splash = add_name(names, &used, "SplashScreen.png");
    wide = add_name(names, &used, "SplashScreen_16x9.png");
    write_dir(tree, root, 1, 1);
    write_dir(tree + 22, skins, 1, 2);
    write_dir(tree + 44, embedded, 1, 3);
    write_dir(tree + 66, img, 2, 4);
    write_file(tree + 88, splash, 0);
    write_file(tree + 110, wide, 16);
    put32(data, 8);
    memcpy(data + 4, "STOCKPNG", 8);
    put32(data + 16, 8);
    memcpy(data + 20, "SIXTEENX", 8);
    memcpy(tree_before, tree, sizeof tree);
    splash_visit(2, tree, names, data);
    expect(memcmp(tree, tree_before, sizeof tree) == 0, "missing file keeps every qrc node");
    expect(memcmp(data + 4, "STOCKPNG", 8) == 0, "missing file keeps stock payload");
    expect(memcmp(data + 20, "SIXTEENX", 8) == 0, "16x9 payload stays untouched");
}

static void test_visit_swaps_only_the_second_stage(const char *png_path) {
    extern const char *splash_override_file;
    unsigned char names[256];
    unsigned char tree[22 * 6];
    unsigned char *data = malloc(32);
    unsigned char wide_before[22];
    expect(data != NULL, "visit data buffer");
    if (!data)
        return;
    unsigned used = 0;
    unsigned offset;
    memset(names, 0, sizeof names);
    write_dir(tree, add_name(names, &used, ""), 1, 1);
    write_dir(tree + 22, add_name(names, &used, "Skins"), 1, 2);
    write_dir(tree + 44, add_name(names, &used, "Embedded"), 1, 3);
    write_dir(tree + 66, add_name(names, &used, "img"), 2, 4);
    write_file(tree + 88, add_name(names, &used, "SplashScreen.png"), 0);
    write_file(tree + 110, add_name(names, &used, "SplashScreen_16x9.png"), 16);
    tree[88 + 5] = 0x01;
    put32(data, 8);
    memcpy(data + 4, "STOCKPNG", 8);
    put32(data + 16, 8);
    memcpy(data + 20, "SIXTEENX", 8);
    memcpy(wide_before, tree + 110, 22);
    splash_override_file = png_path;
    splash_visit(2, tree, names, data);
    splash_override_file = 0;
    offset = be32(tree + 88 + 10);
    expect(offset != 0 && offset < 0x80000000u, "visit relocation stays inside the sxtw window");
    expect((tree[88 + 5] & 0x05) == 0, "visit clears compression on the splash node");
    expect(tree[88 + 6] == 0xab, "visit keeps the splash node metadata");
    expect(memcmp(tree + 110, wide_before, 22) == 0, "visit does not touch SplashScreen_16x9.png");
    expect(memcmp(data + 4, "STOCKPNG", 8) == 0, "visit leaves the original slot bytes");
    expect(memcmp(data + 20, "SIXTEENX", 8) == 0, "visit leaves the 16x9 payload");
    free(data);
}

static void test_stretch_layout(void) {
    const char *source =
        "prelude\n"
        "    sourceComponent: FocusScope {\n"
        "        id: splashscreen\n"
        "\n"
        "        AIRMouseEventBlocker {}\n"
        "        Image {\n"
        "            source: PlanckGUI.views.splashScreenController.viewModel.hasConfirmation\n"
        "                ? kept\n"
        "        }\n"
        "        Image {\n"
        "            id: overlay\n"
        "            source: overlay-source\n"
        "        }\n"
        "        SequentialAnimation {\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "            running: true\n"
        "        }\n"
        "            sourceComponent: SplashScreenWithAcknowledgement {}\n"
        "            active: PlanckGUI.views.splashScreenController.viewModel.hasConfirmation\n"
        "        }\n";
    char *buffer = malloc(strlen(source) + 1);
    char *untouched;
    unsigned len;
    expect(buffer != NULL, "stretch buffer");
    if (!buffer)
        return;
    len = (unsigned)strlen(source);
    memcpy(buffer, source, len);
    untouched = malloc(len);
    expect(untouched != NULL, "stretch control buffer");
    if (!untouched) {
        free(buffer);
        return;
    }
    memcpy(untouched, source, len);
    buffer[20] = 'X';
    expect(!splash_stretch_layout((unsigned char *)buffer, len), "broken pattern is rejected");
    buffer[20] = untouched[20];
    memcpy(buffer, untouched, len);
    expect(splash_stretch_layout((unsigned char *)buffer, len), "synthetic layout stretches");
    buffer[len] = 0;
    expect(strstr(buffer, "anchors.fill: parent") != NULL, "parent fill inserted");
    expect(strstr(buffer, "fillMode: Image.Stretch") != NULL, "stretch fill mode inserted");
    expect(strstr(buffer, "id: overlay") != NULL, "overlay image kept");
    expect(strstr(buffer, "overlay-source") != NULL, "overlay source kept");
    expect(strstr(buffer, "fillMode: Image.Stretch") < strstr(buffer, "id: overlay"), "stretch applies to the boot image");
    expect(strstr(strstr(buffer, "id: overlay"), "fillMode") == NULL, "overlay is not stretched");
    expect(strstr(buffer, "file:///data/prime-unlimited/os-version.qml") != NULL, "version file matches About");
    expect(strstr(buffer, "Prime Unlimited OS ") != NULL, "version label matches About");
    free(untouched);
    free(buffer);
}

static void test_stretch_engine_qml(void) {
    FILE *in = fopen("/tmp/Engine.bin", "rb");
    unsigned char *file;
    long size;
    long at;
    unsigned char *qml;
    const unsigned char *second;
    if (!in)
        return;
    if (fseek(in, 0, SEEK_END) != 0) {
        fclose(in);
        return;
    }
    size = ftell(in);
    if (size < 3000 || fseek(in, 0, SEEK_SET) != 0) {
        fclose(in);
        return;
    }
    file = malloc((unsigned long)size);
    expect(file != NULL && fread(file, 1, (size_t)size, in) == (size_t)size, "read Engine.bin");
    fclose(in);
    if (!file)
        return;
    for (at = 0; at + 4 + 38 < size; at++) {
        if (file[at] == 0 && file[at + 1] == 0 && file[at + 2] == 0x09 && file[at + 3] == 0x45 &&
            memcmp(file + at + 4, "import QtQuick 2.0\nimport airQuick 1.0", 38) == 0)
            break;
    }
    expect(at + 4 + 2373 <= size, "Engine splash qml located");
    if (at + 4 + 2373 > size) {
        free(file);
        return;
    }
    qml = malloc(2374);
    expect(qml != NULL, "qml buffer");
    if (!qml) {
        free(file);
        return;
    }
    memcpy(qml, file + at + 4, 2373);
    qml[2373] = 0;
    free(file);
    expect(splash_stretch_layout(qml, 2373), "engine qml stretches in place");
    expect(strstr((char *)qml, "anchors.fill: parent") != NULL, "engine parent fill");
    expect(strstr((char *)qml, "fillMode: Image.Stretch") != NULL, "engine stretch mode");
    expect(strstr((char *)qml, "SplashScreen_NoText") != NULL, "confirmation image name kept");
    expect(strstr((char *)qml, "splashscreenLoader.source") != NULL, "boot source kept");
    second = (const unsigned char *)strstr((char *)qml, "id: overlay");
    expect(second != NULL, "engine overlay remains");
    expect(second && strstr((char *)second, "fillMode") == NULL, "engine overlay is not restyled");
    expect(strstr((char *)qml, "file:///data/prime-unlimited/os-version.qml") != NULL, "engine version file");
    expect(strstr((char *)qml, "anchors.right: parent.right") != NULL, "engine version sits on the right");
    expect(strstr((char *)qml, "anchors.bottom: parent.bottom") != NULL, "engine version sits on the bottom");
    expect(strstr((char *)qml, "Prime Unlimited OS ") != NULL, "engine version label matches About");
    free(qml);
}

int main(int argc, char **argv) {
    if (argc != 2) {
        fprintf(stderr, "usage: splash_resource_host PNG\n");
        return 2;
    }
    test_paths_and_png();
    test_stamp_and_inplace();
    test_relocation_rules();
    test_map_and_real_png(argv[1]);
    test_absent_file_keeps_stock();
    test_visit_swaps_only_the_second_stage(argv[1]);
    test_stretch_layout();
    test_stretch_engine_qml();
    if (failures) {
        fprintf(stderr, "%d failure(s)\n", failures);
        return 1;
    }
    puts("splash resource host tests passed");
    return 0;
}
