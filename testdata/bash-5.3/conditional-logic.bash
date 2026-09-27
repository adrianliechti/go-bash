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
# Selected from GNU Bash 5.3 tests/cond.tests, lines 18, 24-31, 36-62, 68-88, 146-154, 173-187, 229-241.
# Excerpts selected for go-bash on 2026-09-27.
# See README.md for provenance, adaptations, and omitted coverage.

TDIR=/usr/homes/chet

# [[ x ]] is equivalent to [[ -n x ]]
[[ x ]]
echo returns: $?

# [[ ! x ]] is equivalent to [[ ! -n x ]]
[[ ! x ]]
echo returns: $?


# ! toggles on and off rather than just setting an `invert result' flag
# this differs from ksh93
[[ ! 1 -eq 1 ]]; echo $?
[[ ! ! 1 -eq 1 ]]; echo $?

[[ ! ! ! 1 -eq 1 ]]; echo $?
[[ ! ! ! ! 1 -eq 1 ]]; echo $?

# parenthesized terms didn't work right until post-2.04
[[ a ]]
echo returns: $?

[[ (a) ]]
echo returns: $?

[[ -n a ]]
echo returns: $?

[[ (-n a) ]]
echo returns: $?

# unset variables don't need to be quoted
[[ -n $UNSET ]]
echo returns: $?

[[ -z $UNSET ]]
echo returns: $?

# the ==/= and != operators do pattern matching
[[ $TDIR == /usr/homes/* ]]
echo returns: $?

# ...but you can quote any part of the pattern to have it matched as a string
[[ $TDIR == /usr/homes/\* ]]
echo returns: $?

[[ $TDIR == '/usr/homes/*' ]]
echo returns: $?

# if the first part of && fails, the second is not executed
[[ -n $UNSET && $UNSET == foo ]]
echo returns: $?

[[ -z $UNSET && $UNSET == foo ]]
echo returns: $?

# if the first part of || succeeds, the second is not executed
[[ -z $UNSET || -d $PWD ]]
echo returns: $?

# more pattern matching tests

[[ $filename == *.c ]]
echo returns: $?

filename=patmatch.c

[[ $filename == *.c ]]
echo returns: $?

# make sure the null string is never matched if the string is not null
STR=file.c
PAT=

if [[ $STR = $PAT ]]; then
        echo oops
fi

# but that if the string is null, a null pattern is matched correctly
STR=
PAT=

if [[ $STR = $PAT ]]; then
        echo ok
fi

# allow reserved words after a conditional command just because
if [[ str ]] then [[ str ]] fi

# make sure pattern matching does the right thing with CTLESC
var=$'ab\001'
[[ $var == $var ]] && echo ok c1
[[ $var == a* ]] && echo ok c2
[[ $var == $'ab\001' ]] && echo ok c3

var=$'ab\001c'
[[ $var == $var ]] && echo ok c4
[[ $var == a* ]] && echo ok c5
[[ $var == $'ab\001'* ]] && echo ok c6
