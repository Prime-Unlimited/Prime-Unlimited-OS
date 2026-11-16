"""Deterministic, hardware-independent catalog dependency resolution."""
import re
from dataclasses import dataclass

from registry import ModError, _digest, _id


VERSION = re.compile(r"^(0|[1-9][0-9]*)(?:\.(0|[1-9][0-9]*))?(?:\.(0|[1-9][0-9]*))?(?:-([0-9A-Za-z.-]+))?$")


def version_key(value):
    if not isinstance(value, str):
        raise ModError("invalid version")
    match = VERSION.fullmatch(value)
    if not match:
        raise ModError("unsupported version: " + value)
    major, minor, patch, pre = match.groups()
    # Stable versions sort after prereleases with the same numeric tuple.
    return (int(major), int(minor or 0), int(patch or 0), 1 if pre is None else 0, pre or "")


def satisfies(version, constraint):
    """Support exact, comma-separated comparisons, caret, tilde and wildcard."""
    if constraint in (None, "", "*"):
        return True
    actual = version_key(version)
    for raw in str(constraint).split(","):
        term = raw.strip()
        if not term:
            continue
        if term.endswith(".*"):
            prefix = term[:-2].split(".")
            nums = actual[:len(prefix)]
            if nums != tuple(int(x) for x in prefix):
                return False
            continue
        if term[0] in "^~":
            base = version_key(term[1:])
            if actual < base:
                return False
            if term[0] == "^":
                boundary = (base[0] + 1, 0, 0) if base[0] else (0, base[1] + 1, 0)
            else:
                boundary = (base[0], base[1] + 1, 0)
            if actual[:3] >= boundary:
                return False
            continue
        match = re.match(r"^(<=|>=|==|=|<|>)?\s*(.+)$", term)
        if not match:
            raise ModError("invalid version constraint: " + term)
        op, wanted = match.groups()
        expected = version_key(wanted)
        op = op or "="
        valid = {"=": actual == expected, "==": actual == expected,
                 ">=": actual >= expected, "<=": actual <= expected,
                 ">": actual > expected, "<": actual < expected}[op]
        if not valid:
            return False
    return True


def compatible(record, device):
    if record.get("revoked"):
        return False
    models = record.get("models", record.get("devices", []))
    firmware = record.get("firmware", [])
    if device.get("model") not in models:
        return False
    fw_constraint = record.get("firmwareConstraint")
    if fw_constraint:
        if not satisfies(device.get("firmware", ""), fw_constraint):
            return False
    elif device.get("firmware") not in firmware:
        return False
    return (record.get("architecture") in (None, device.get("architecture")) and
            record.get("abi") in (None, device.get("abi")))


@dataclass(frozen=True)
class Resolution:
    mods: tuple
    libraries: tuple
    order: tuple


class Resolver:
    def __init__(self, catalog, device):
        self.device = dict(device)
        self.mods = self._index(catalog.get("mods", []), "mod")
        self.libs = self._index(catalog.get("libraries", []), "library")

    @staticmethod
    def _index(records, kind):
        result = {}
        for item in records:
            if not isinstance(item, dict):
                raise ModError("malformed %s catalog record" % kind)
            ident, version = _id(item.get("id")), item.get("version")
            version_key(version)
            result.setdefault(ident, []).append(item)
        for values in result.values():
            values.sort(key=lambda x: version_key(x["version"]), reverse=True)
        return result

    def _select(self, index, kind, ident, constraints):
        options = [x for x in index.get(ident, [])
                   if all(satisfies(x["version"], c) for c in constraints)
                   and compatible(x, self.device)]
        if not options:
            available = ", ".join(x["version"] for x in index.get(ident, [])) or "none"
            raise ModError("no compatible %s %s satisfies %s (available: %s)" %
                           (kind, ident, " & ".join(constraints) or "*", available))
        chosen = options[0]
        if kind == "library":
            _digest(chosen.get("sha256"))
        return chosen

    def resolve(self, requests, installed=None):
        roots = {}
        for request in requests:
            ident = _id(request["id"])
            roots.setdefault(ident, []).append(request.get("version", "*"))

        initial = {"mod": {key: list(value) for key, value in roots.items()}, "library": {}}

        def solve(constraints, selected):
            for kind in ("mod", "library"):
                for ident, record in selected[kind].items():
                    if not all(satisfies(record["version"], value)
                               for value in constraints[kind].get(ident, [])):
                        return None
            pending = [(kind, ident) for kind in ("mod", "library")
                       for ident in sorted(constraints[kind]) if ident not in selected[kind]]
            if not pending:
                return selected
            kind, ident = pending[0]
            index = self.mods if kind == "mod" else self.libs
            candidates = [item for item in index.get(ident, [])
                          if compatible(item, self.device) and
                          all(satisfies(item["version"], value)
                              for value in constraints[kind][ident])]
            for current in candidates:
                if kind == "mod":
                    conflicts = {x if isinstance(x, str) else x.get("id")
                                 for x in current.get("conflicts", [])}
                    reverse = {x if isinstance(x, str) else x.get("id")
                               for item in selected["mod"].values()
                               for x in item.get("conflicts", [])}
                    if conflicts.intersection(set(selected["mod"]) | set(installed or {})) or ident in reverse:
                        continue
                if kind == "library":
                    _digest(current.get("sha256"))
                next_constraints = {group: {key: list(value) for key, value in groups.items()}
                                    for group, groups in constraints.items()}
                next_selected = {group: dict(values) for group, values in selected.items()}
                next_selected[kind][ident] = current
                dependencies = list(current.get("dependencies", []))
                if kind == "mod":
                    dependencies += [{**d, "type": "library"} for d in current.get("libraries", [])]
                for dep in dependencies:
                    dep_kind = dep.get("type", "mod")
                    if dep_kind not in next_constraints:
                        raise ModError("invalid dependency type")
                    did = _id(dep["id"])
                    requirement = dep.get("constraint", dep.get("version", "*"))
                    next_constraints[dep_kind].setdefault(did, []).append(requirement)
                answer = solve(next_constraints, next_selected)
                if answer is not None:
                    return answer
            return None

        selected = solve(initial, {"mod": {}, "library": {}})
        if selected is None:
            details = ", ".join("%s %s" % (key, " & ".join(value))
                                for key, value in sorted(roots.items()))
            raise ModError("dependency constraints cannot be satisfied: " + details)

        visiting, visited, order = set(), set(), []
        def visit(kind, ident):
            node = (kind, ident)
            if node in visiting:
                raise ModError("cyclic dependency involving %s %s" % node)
            if node in visited:
                return
            visiting.add(node)
            record = selected[kind][ident]
            for conflict in record.get("conflicts", []):
                cid = conflict if isinstance(conflict, str) else conflict.get("id")
                if cid in selected["mod"] or (installed and cid in installed):
                    raise ModError("%s conflicts with installed or selected mod %s" % (ident, cid))
            dependencies = list(record.get("dependencies", []))
            if kind == "mod":
                dependencies += [{**d, "type": "library"} for d in record.get("libraries", [])]
            for dep in sorted(dependencies, key=lambda x: (x.get("type", "mod"), x["id"])):
                visit(dep.get("type", "mod"), dep["id"])
            visiting.remove(node); visited.add(node); order.append(node)
        for ident in sorted(roots):
            visit("mod", ident)
        return Resolution(tuple(selected["mod"][x] for x in sorted(selected["mod"])
                                if ("mod", x) in visited),
                          tuple(selected["library"][x] for x in sorted(selected["library"])
                                if ("library", x) in visited),
                          tuple(order))
