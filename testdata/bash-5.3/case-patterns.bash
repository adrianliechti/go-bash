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
# Selected from GNU Bash 5.3 tests/case.tests, lines 45-76.
# Excerpts selected for go-bash on 2026-09-27.
# See README.md for provenance, adaptations, and omitted coverage.

unset var empty

var=
case ']' in
[$var]*[$var])	echo matches 1;;
*)		echo no match 1 ;;
esac

case abc in ( [] ) echo yes ;; ( * ) echo no ;; esac
empty=''
case abc in ( ["$empty"] ) echo yes ;; ( * ) echo no ;; esac

case abc in ( [] | [!a-z]* ) echo yes ;; ( * ) echo no ;; esac
empty=''
case abc in ( ["$empty"] | [!a-z]* ) echo yes ;; ( * ) echo no ;; esac

case abc in (["$empty"]|[!a-z]*) echo yes ;; (*) echo no ;; esac

case " " in ( [" "] ) echo ok;; ( * ) echo no;; esac

# posix issue discovered after bash-5.1 was released
case esac in (esac) echo esac;; esac
case k in else|done|time|esac) for f in 1 2 3 ; do :; done esac

# null words and patterns
var=value
case $unset in
'')	echo unset word ok 1 ;;&
$unset|$var)	echo unset word ok 2 ;;&
unset|$unset)	echo unset word ok 3 ;;
*)	echo unset word bad ;;
esac
