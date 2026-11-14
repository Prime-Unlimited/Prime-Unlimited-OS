/*
 * ALSA boot hook for Denon Prime 4 G2 (JP24).
 * Loaded through the writable /data ALSA configuration on each boot.
 * Keep the symbol version names exactly as required by the Cd1s fix.
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

/* Run at dlopen time, before ALSA invokes the configured hook. */
__attribute__((constructor))
static void alsa_boot_hook_constructor(void) {
    activate_once();
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
