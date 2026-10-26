package cdp

import "time"

// nodeFixtureTimeout 盖过 CI 上并行跑 go test 时的情况。
// 这些夹具本机几十毫秒就结束，但 5 秒上限会在 node 还没打印结果时把它 SIGKILL 掉。
const nodeFixtureTimeout = 30 * time.Second
