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
# Ported from GNU Bash 5.3 tests/varenv9.sub, lines 14-25, 32-44.
# Selected 2026-09-27; commands in the excerpts are unchanged.

# case 1: readonly modifying local scalar variables
o1() {
    local i1 j1
    readonly i1=$1
    readonly j1="1 2 3"

    echo "in o1 (readonly modifying local scalars):"
    declare -p i1
    declare -p j1
}

o1 "a b c"

# case 2: readonly setting global scalar variables
o2() {
    readonly i2=$1
    readonly j2="1 2 3"

    echo "in o2 (readonly setting global scalars):"
    declare -p i2
    declare -p j2
}

o2 "a b c"
echo after o2:
declare -p i2 j2
