/*
 * ALSA boot hook for Denon Prime 4 G2 (JP24).
 *
 * Loaded through the writable /data ALSA configuration on each boot:
 * /data/system/var-lib/alsa/conf.d/99-prime4-mod.conf is bind-mounted to
 * /var/lib/alsa/conf.d/ (systemd's var-lib.mount unit), which ALSA's
 * top-level @hooks [ { func load ... } ] entry in /usr/share/alsa/alsa.conf
 * loads on first ALSA client call. Our conf.d file declares its own
 * hook_func + @hooks, which gets evaluated as it is merged into the tree,
 * triggering dlopen() of this library.
 *
 * Two things must be exactly right for this to work on this device:
 *
 * 1. Build for glibc, not musl ("-target aarch64-linux-gnu.2.17", not
 *    "-musl"). The device's libc is glibc; a musl-linked .so requests
 *    "libc.so" as NEEDED, which does not exist, so dlopen() fails every
 *    boot with no visible error (ALSA just silently never loads its
 *    hooks). musl wants "libc.so", glibc provides "libc.so.6" - fixing
 *    the target fixes this categorically.
 * 2. Keep the Cd1s symbol-name fix: the plain function name
 *    (prime4_mod_alsa_hook) PLUS the snd_config_hook_-prefixed wrapper
 *    PLUS both _dlsym_config_hook_001 version-tag data symbols. Earlier
 *    revisions without these broke the device's LED/button control
 *    (shared ALSA symbol namespace with Engine's own code) even though
 *    the hook itself still loaded.
 *
 * fork()/execve() from inside this hook are NOT blocked (no seccomp on
 * this device) - this was independently confirmed by reaching a live
 * root shell. Earlier failed attempts were caused entirely by #1 above
 * (hook never loading in the first place), not an exec restriction.
 */
#include <fcntl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#define ACTIVATE "/data/prime4-mod/boot-activate.sh"
#define STAMP    "/run/prime4-mod-activated"

const char _prime4_mod_alsa_hook_dlsym_config_hook_001[] = "ALSA_0.9";
const char _snd_config_hook_prime4_mod_alsa_hook_dlsym_config_hook_001[] = "ALSA_0.9";

static void activate_once(void) {
    struct stat st;
    if (stat(ACTIVATE, &st) != 0 || !S_ISREG(st.st_mode))
        return;

    /* O_EXCL makes the once-per-boot guard safe if ALSA loads concurrently. */
    int fd = open(STAMP, O_WRONLY | O_CREAT | O_EXCL, 0600);
    if (fd < 0)
        return;
    close(fd);

    pid_t pid = fork();
    if (pid == 0) {
        (void)setsid();
        execl("/bin/sh", "sh", ACTIVATE, (char *)0);
        _exit(127);
    }
}

int prime4_mod_alsa_hook(void *root, void *config, void **dst, void *private_data) {
    (void)root;
    (void)config;
    (void)private_data;
    if (dst)
        *dst = 0;
    activate_once();
    return 0;
}

int snd_config_hook_prime4_mod_alsa_hook(void *root, void *config, void **dst, void *private_data) {
    return prime4_mod_alsa_hook(root, config, dst, private_data);
}
