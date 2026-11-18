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
 * Confirmed end to end on real hardware, fully automatically on a fresh
 * flash and reboot: the hook fires, launches boot-activate.sh, and a
 * systemd-managed sshd service accepts an authenticated login with the
 * enrolled key. Keeping sshd in its own systemd unit matters because an
 * Engine restart can kill processes started in Engine's cgroup. Four
 * things had to be exactly right, each of
 * which failed completely silently when wrong:
 *
 * 1. Build for glibc, not musl ("-target aarch64-linux-gnu.2.17", not
 *    "-musl"). The device's libc is glibc; a musl-linked .so requests
 *    "libc.so" as NEEDED, which does not exist, so dlopen() fails every
 *    boot with no visible error (ALSA just silently never loads its
 *    hooks). musl wants "libc.so", glibc provides "libc.so.6".
 * 2. Keep the Cd1s symbol-name fix: the plain function name
 *    (prime4_mod_alsa_hook) PLUS the snd_config_hook_-prefixed wrapper
 *    PLUS both _dlsym_config_hook_001 version-tag data symbols. Earlier
 *    revisions without these broke the device's LED/button control
 *    (shared ALSA symbol namespace with Engine's own code) even though
 *    the hook itself still loaded.
 * 3. boot-activate.sh creates /var/run/sshd before starting the unit.
 * 4. The systemd unit invokes /usr/sbin/sshd by its absolute path, as
 *    required for sshd privilege separation.
 *
 * fork()/execve() from inside this hook are NOT blocked (no seccomp on
 * this device) - confirmed by the above actually working end to end.
 */
#include <fcntl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <signal.h>
#include <unistd.h>

#define STAMP "/run/prime4-mod-activated"

const char _prime4_mod_alsa_hook_dlsym_config_hook_001[] = "ALSA_0.9";
const char _snd_config_hook_prime4_mod_alsa_hook_dlsym_config_hook_001[] = "ALSA_0.9";

static void start_ssh_service(void) {
    pid_t pid = fork();
    if (pid != 0)
        return;
    (void)setsid();

    int devnull = open("/dev/null", O_RDWR);
    if (devnull >= 0) {
        dup2(devnull, 0);
        dup2(devnull, 1);
        dup2(devnull, 2);
        if (devnull > 2)
            close(devnull);
    }
    /* SIG_IGN dispositions survive exec(); a dead inherited stdout can't
     * kill the process with SIGPIPE before it gets a chance to run. */
    signal(SIGPIPE, SIG_IGN);

    execl("/bin/sh", "/bin/sh", "/data/prime4-mod/boot-activate.sh", (char *)0);
    _exit(127);
}

static void activate_once(void) {
    /* O_EXCL makes the once-per-boot guard safe if ALSA loads concurrently. */
    int fd = open(STAMP, O_WRONLY | O_CREAT | O_EXCL, 0600);
    if (fd < 0)
        return;
    close(fd);

    struct stat st;
    if (stat("/data/prime4-mod/authorized_keys", &st) != 0 || st.st_size == 0)
        return;

    start_ssh_service();
}

#define MODLOADER_STAMP "/run/prime4-mod-ui-activated"
#define MODLOADER_SCRIPT "/data/prime-unlimited/bin/activate-jp24.sh"

/* Entirely independent of activate_once()/start_sshd() above: separate
 * fork, separate once-per-boot stamp, separate optional script. Installing
 * or breaking the mod loader must never be able to affect SSH access -
 * see modloader/device/jp24/ui-hook/activate-jp24.sh for what this runs. */
static void start_modloader_activation(void) {
    int fd = open(MODLOADER_STAMP, O_WRONLY | O_CREAT | O_EXCL, 0600);
    if (fd < 0)
        return;
    close(fd);

    struct stat st;
    if (stat(MODLOADER_SCRIPT, &st) != 0 || st.st_size == 0)
        return; /* not installed on this device - do nothing */

    pid_t pid = fork();
    if (pid != 0)
        return;
    (void)setsid();

    int devnull = open("/dev/null", O_RDWR);
    if (devnull >= 0) {
        dup2(devnull, 0);
        dup2(devnull, 1);
        dup2(devnull, 2);
        if (devnull > 2)
            close(devnull);
    }
    signal(SIGPIPE, SIG_IGN);

    execl("/bin/sh", "/bin/sh", MODLOADER_SCRIPT, (char *)0);
    _exit(127);
}

int prime4_mod_alsa_hook(void *root, void *config, void **dst, void *private_data) {
    (void)root;
    (void)config;
    (void)private_data;
    if (dst)
        *dst = 0;
    activate_once();
    start_modloader_activation();
    return 0;
}

int snd_config_hook_prime4_mod_alsa_hook(void *root, void *config, void **dst, void *private_data) {
    return prime4_mod_alsa_hook(root, config, dst, private_data);
}
