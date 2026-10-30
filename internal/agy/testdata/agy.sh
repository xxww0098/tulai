#!/bin/sh
# 测试用的假 agy。模式写在 $HOME/fixture.txt，不回显授权码。
set -eu
mode="ok"
if [ -f "$HOME/fixture.txt" ]; then
  mode=$(tr -d '[:space:]' < "$HOME/fixture.txt")
fi
# 导入验证时 HOME 是新建的账号目录，模式从环境变量传入，不写进凭证。
if [ -n "${AGY_FIXTURE:-}" ]; then
  mode=$AGY_FIXTURE
fi
# 记下 cwd 和提示词，测试用来确认参考图路径落在工作目录里。
printf '%s\n' "$PWD" > "$HOME/last-cwd.txt"
prompt=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-p" ]; then
    prompt=$arg
  fi
  prev=$arg
done
printf '%s\n' "$prompt" > "$HOME/last-prompt.txt"
marker="edit the reference image "
case "$prompt" in
  *"$marker"*)
    tail=${prompt#*"$marker"}
    refpath=${tail%%: *}
    case "$refpath" in
      "$PWD"/*)
        if [ -f "$refpath" ]; then
          cp "$refpath" "$HOME/staged-ref"
        fi
        ;;
    esac
    ;;
esac
root="$HOME/.gemini/antigravity-cli"
cid="conv-test"
case "$mode" in
  quota)
    printf '%s\n' '{"event":"init","conversation_id":"conv-quota"}'
    printf '%s\n' '{"event":"error","error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"quota exceeded"}}'
    printf '%s\n' '{"event":"result","result":{"status":"ERROR","response":""}}'
    exit 0
    ;;
  quota-reset)
    printf '%s\n' '{"event":"error","error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"rate limit","retryDelay":"3600s"}}'
    printf '%s\n' '{"event":"result","result":{"status":"ERROR","response":""}}'
    exit 0
    ;;
  unauth)
    printf '%s\n' 'Authentication required. Please visit the URL to log in:'
    printf '%s\n' 'https://accounts.google.com/o/oauth2/auth?client_id=agy'
    printf '%s\n' 'Or, paste the authorization code here and press Enter:'
    # 用内建 read 挂起，进程被杀掉时管道会马上关闭。
    read -t 30 _hold || true
    exit 0
    ;;
  login)
    printf '%s\n' 'Authentication required. Please visit the URL to log in:'
    printf '%s\n' 'https://accounts.google.com/o/oauth2/auth?client_id=agy'
    printf '%s\n' 'Or, paste the authorization code here and press Enter:'
    set +e
    IFS= read -r _code
    set -e
    unset _code
    mkdir -p "$root"
    : > "$root/antigravity-oauth-token"
    printf '%s\n' 'OAuth: authenticated successfully as someone@gmail.com' >> "$root/cli.log"
    exit 0
    ;;
  hang)
    printf '%s\n' 'Authentication required. Please visit the URL to log in:'
    printf '%s\n' 'https://accounts.google.com/o/oauth2/auth?client_id=agy'
    printf '%s\n' 'Or, paste the authorization code here and press Enter:'
    read -t 30 _hold || true
    exit 0
    ;;
  deny)
    printf '%s\n' '{"event":"init","conversation_id":"conv-deny"}'
    printf '%s\n' 'user denied permission for read_file(/workspace/agy-test/poster.png)'
    printf '%s\n' '{"event":"result","result":{"status":"SUCCESS","response":""}}'
    exit 0
    ;;
  fail)
    printf '%s\n' '{"event":"result","result":{"status":"ERROR","response":""}}'
    exit 0
    ;;
  *)
    mkdir -p "$root/brain/$cid/.tempmediaStorage"
    printf 'skip' > "$root/brain/$cid/.tempmediaStorage/junk.jpg"
    sample="$HOME/sample.jpg"
    out="$root/brain/$cid/image_1.jpg"
    if [ -f "$sample" ]; then
      cp "$sample" "$out"
    else
      printf 'x' > "$out"
    fi
    printf '%s\n' "{\"event\":\"init\",\"conversation_id\":\"$cid\"}"
    printf '%s\n' "{\"event\":\"result\",\"result\":{\"status\":\"SUCCESS\",\"response\":\"$out\n\"}}"
    exit 0
    ;;
esac
