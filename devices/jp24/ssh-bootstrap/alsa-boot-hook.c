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
 * flash and reboot: the hook fires, forks sshd directly (no shell, no
 * systemd unit needed), and a real authenticated SSH login with the
 * enrolled key succeeds. Four things had to be exactly right, each of
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
 * 3. sshd refuses to start without its privilege-separation directory,
 *    which systemd's sshd.service normally creates via tmpfiles.d before
 *    ever starting sshd - "Missing privilege separation directory:
 *    /var/run/sshd", logged only to sshd's own stderr, which nothing was
 *    capturing by default. sshd exits immediately (visible as a
 *    <defunct> zombie), fork() and execl() both still "succeed" from
 *    this code's point of view, and the port simply never opens. mkdir
 *    it first.
 * 4. execl()'s argv[0] for sshd MUST be the absolute path
 *    ("/usr/sbin/sshd"), not just "sshd" - sshd re-execs itself for
 *    privilege separation and refuses with "sshd re-exec requires
 *    execution with an absolute path" otherwise, again only visible on
 *    its own stderr. This is also why running the identical command
 *    manually from an interactive shell always worked during debugging:
 *    a shell sets argv[0] to exactly what was typed, which was always
 *    the full path - the bug was specific to the automatic path.
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

static void start_sshd(void) {
    pid_t pid = fork();
    if (pid != 0)
        return;
    (void)setsid();

    mkdir("/var/run/sshd", 0755);

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

    execl("/usr/sbin/sshd", "/usr/sbin/sshd",
          "-D",
          "-f", "/dev/null",
          "-o", "HostKey /etc/ssh/ssh_host_ed25519_key",
          "-o", "AuthorizedKeysFile /data/prime4-mod/authorized_keys",
          "-o", "PermitRootLogin yes",
          "-o", "PubkeyAuthentication yes",
          "-o", "PasswordAuthentication no",
          "-o", "KbdInteractiveAuthentication no",
          "-o", "StrictModes no",
          "-o", "Port 2222",
          (char *)0);
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

    start_sshd();
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
