#!/bin/sh
# Prime Unlimited wrapper that injects the UI hook preload
export LD_PRELOAD=/data/prime-unlimited/lib/libprime-ui-hook.so${LD_PRELOAD:+:$LD_PRELOAD}
exec /data/prime-unlimited/bin/runengine.real "$@"
