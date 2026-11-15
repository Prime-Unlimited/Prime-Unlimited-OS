/* Independent ARMv7 Linux ALSA hook; no libc, libasound or vendor dependencies.
 * ARM EABI syscalls avoid coupling the hook to the build host's glibc version.
 */
static long call(long number, long a, long b, long c) {
    register long r7 __asm__("r7") = number;
    register long r0 __asm__("r0") = a;
    register long r1 __asm__("r1") = b;
    register long r2 __asm__("r2") = c;
    register long r3 __asm__("r3") = 0;
    __asm__ volatile("svc 0" : "+r"(r0) : "r"(r7), "r"(r1), "r"(r2), "r"(r3) : "memory", "cc");
    return r0;
}
int prime_ssh_hook(void *root, void *config, void **result, void *private_data) {
    (void)root; (void)config; (void)private_data;
    if (result) *result = 0;
    long child = call(2, 0, 0, 0); /* fork */
    if (child == 0) {
        long worker = call(2, 0, 0, 0);
        if (worker == 0) {
            const char *args[] = {"sh", "/data/prime-ssh/activate.sh", 0};
            const char *env[] = {"PATH=/usr/sbin:/usr/bin:/sbin:/bin", 0};
            call(66, 0, 0, 0); /* setsid */
            call(11, (long)"/bin/sh", (long)args, (long)env);
            call(1, 127, 0, 0);
        }
        call(1, worker < 0, 0, 0);
    }
    if (child > 0) call(114, child, 0, 0); /* wait4 */
    return 0;
}
/* ALSA SND_DLSYM_BUILD_VERSION configuration-hook marker. */
char _prime_ssh_hook_dlsym_config_hook_001;
