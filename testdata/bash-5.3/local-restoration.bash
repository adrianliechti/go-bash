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
# Ported from GNU Bash 5.3 tests/varenv.tests, lines 120-139, 164-171, 267-268.
# Selected 2026-09-27; commands in the excerpts are unchanged.

func()
{
	local YYZ

	YYZ="song by rush"
	echo $YYZ
	echo $A
}

YYZ="toronto airport"
A="AVAR"
echo $YYZ
echo $A
A=BVAR func
echo $YYZ
echo $A

export A
# Make sure expansion doesn't use assignment statements preceding a builtin
A=ZVAR echo $A

# export an unset variable, make sure it is not suddenly set, but make
# sure the export attribute persists when the variable is assigned a
# value
export ivar
echo ${ivar-unset}

ivar=42
declare -p ivar

# make sure variable scoping is done right
tt() { typeset a=b;echo a=$a; };a=z;echo a=$a;tt;echo a=$a
