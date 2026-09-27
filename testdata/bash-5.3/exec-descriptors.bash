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
# Ported from GNU Bash 5.3 tests/redir.tests, lines 63-70, 74-78, 82-90, 175-178, 210-217.
# Selected 2026-09-27; commands in the excerpts are unchanged.

exec 4>$TMPDIR/bash-a
exec 5>$TMPDIR/bash-b
echo "Point 2"

echo to a 1>&4
echo to b 1>&5
cat $TMPDIR/bash-a
cat $TMPDIR/bash-b

echo to a 1>&4
echo to b 1>&5
cat $TMPDIR/bash-a
cat $TMPDIR/bash-b


exec 6<>$TMPDIR/bash-c
echo to c 1>&6
cat $TMPDIR/bash-c
echo "Point 5"

# clean up before running scripts
exec 4>&- 5>&- 6<&$unset-		# ksh93 quirk with unset variable

rm -f $TMPDIR/bash-a $TMPDIR/bash-b $TMPDIR/bash-c

# These should not echo anything -- bug in versions before 2.04
( ( echo hello 1>&3 ) 3>&1 ) >/dev/null 2>&1

( ( echo hello 1>&3 ) 3>&1 ) >/dev/null 2>&1 | cat

exec 9>&2
command exec 2>>$TMPDIR/foo-$$
echo whatsis >&2
echo cat /tmp/foo
cat $TMPDIR/foo-$$
rm -f $TMPDIR/foo-$$
exec 2>&9
exec 9>&-
