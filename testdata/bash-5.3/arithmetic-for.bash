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
# Selected from GNU Bash 5.3 tests/arith-for.tests, lines 14-55, 68-84, 87, 104-109, 126-129.
# Excerpts selected for go-bash on 2026-09-27.
# See README.md for provenance, adaptations, and omitted coverage.

fx()
{
i=0
for (( ; i < 3; i++ ))
do
	echo $i
done

for (( i=0; ; i++ ))
do
	if (( i >= 3 )); then
		break;
	fi
	echo $i
done

for (( i=0; i<3; ))
do
	echo $i
	(( i++ ))
done

i=0
for (( ; ; ))
do
	if (( i > 2 )); then
		break;
	fi
	echo $i;
	(( i++ ))
done

i=0
for ((;;))
do
	if (( i > 2 )); then
		break;
	fi
	echo $i;
	(( i++ ))
done
}

for (( i=0; ; i++ ))
do
	if (( i >= 3 )); then
		break;
	fi
	echo $i
done

for ((i = 0; ;i++ ))
do
	echo $i
	if (( i < 3 )); then
		(( i++ ))
		continue;
	fi
	break
done

fx

# one-liners added in post-bash-2.04
for     ((i=0; i < 20; i++)) do : ; done
echo $i

for     ((i=0; i < 20; i++)) { : ; }
echo $i

for (( i = 4; ;i--)) ; do echo $i; if (( $i == 0 )); then break; fi; done

for (( i = 4;;i--)) ; do echo $i; if (( $i == 0 )); then break; fi; done
i=4 ; for (( ;;i--)) ; do echo -n $i; if (( i == 0 )); then break; fi; done; echo
