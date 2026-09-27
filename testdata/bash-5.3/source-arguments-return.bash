#   This program is free software: you can redistribute it and/or modify
#   it under the terms of the GNU General Public License as published by
#   the Free Software Foundation, either version 3 of the License, or
#   (at your option) any later version.
#
#   This program is distributed in the hope that it will be useful,
#   but WITHOUT ANY WARRANTY; without even the implied warranty of
#   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
#   GNU General Public License for more details.
#
#   You should have received a copy of the GNU General Public License
#   along with this program.  If not, see <http://www.gnu.org/licenses/>.
#
# Ported from GNU Bash 5.3 tests/builtins.tests, lines 158-181, 194-197.
# Selected 2026-09-27; commands in the excerpts are unchanged.

# and sourcing a file that doesn't end in a newline had better work too
SFILE=$TMPDIR/sourced-file-$$
printf 'echo no-newline' > $SFILE
. $SFILE
rm -f $SFILE
unset -v SFILE

AVAR=AVAR

. ./source1.sub
AVAR=foo . ./source1.sub

. ./source2.sub
echo $?

set -- a b c
. ./source3.sub

# make sure source with arguments does not change the shell's positional
# parameters, but that the sourced file sees the arguments as its
# positional parameters
echo "$@"
. ./source3.sub x y z
echo "$@"

set -- a b c
echo "$@"
. source4.sub x y z
echo "$@"
