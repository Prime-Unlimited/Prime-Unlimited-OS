#ifndef PRIME_UNLIMITED_SPLASH_RESOURCE_H
#define PRIME_UNLIMITED_SPLASH_RESOURCE_H

/* Second-stage Engine splash only. The firmware splash partition is not touched. */
void splash_visit(int version, unsigned char *tree, const unsigned char *names, unsigned char *data);

int splash_path_is_second_stage(const char *path);
int splash_payload_accepted(const unsigned char *png, unsigned png_len);
int splash_relocation_allowed(unsigned long long blob, unsigned long long data, int require_sxtw);
unsigned splash_map_span(unsigned bytes);

/* 1 = payload written into the existing qrc slot. 0 = node left unchanged. */
int splash_commit_inplace(unsigned char *node, int version, unsigned char *slot,
    unsigned slot_cap, const unsigned char *png, unsigned png_len);

/* 1 = node now points at blob. 0 = node left unchanged. blob is 4 + png_len bytes. */
int splash_commit_relocated(unsigned char *node, int version, const unsigned char *data,
    unsigned char *blob, const unsigned char *png, unsigned png_len, int require_sxtw);

/* Fresh mapping whose 32-bit offset from data stays positive (aarch64 sxtw). NULL otherwise. */
unsigned char *splash_map_near(const unsigned char *data, unsigned bytes);

#endif
