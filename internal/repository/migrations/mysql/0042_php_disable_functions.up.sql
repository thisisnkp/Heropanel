-- Per-site PHP disable_functions policy (MariaDB).
--
-- disable_functions is the control that stops a hosted site from shelling out of
-- PHP: exec/system/passthru/proc_open and friends are how a web vulnerability
-- becomes code execution on the box. It belongs on the pool file (the panel's
-- confinement block, rendered last so the php.ini editor can never restore a
-- banned function), and it is a per-site setting because how much a site is
-- allowed to do is a property of its plan, not of the node.
--
-- It is stored as a named policy tier rather than a raw function list so the set
-- is chosen once, centrally (internal/php/settings.go), and cannot drift or be
-- typo'd into something that silently allows exec back. Tiers: 'strict' (the
-- shared-hosting baseline — the full dangerous set disabled), 'basic' (only the
-- process-execution family disabled), 'off' (nothing disabled — trusted sites).
--
-- VARCHAR (not TEXT) so the DEFAULT applies on every MariaDB this panel supports,
-- the same reason ini_overrides is VARCHAR (see 0016).
ALTER TABLE php_pools
    ADD COLUMN func_policy VARCHAR(16) NOT NULL DEFAULT 'strict';
