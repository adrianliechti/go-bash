# Ported from GNU Bash 5.3 tests/builtins7.sub, lines 9-12, 16-17, 22-25, 30, 33-36.
# Selected 2026-09-27; commands in the excerpts are unchanged.

command ; echo $?
command -- ; echo $?
command -p ; echo $?
command -p -- ; echo $?

command -p echo three; echo $?
command -p -- echo four ; echo $?

command -p
echo $?

${THIS_SH} -c 'set -e ; command false ; echo after' bash

command -p command -V type

command -v type
command command -v type
command -p command -v type
command -p -- command -v type
