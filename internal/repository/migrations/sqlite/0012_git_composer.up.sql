-- Composer auto-install for Git deploys (SQLite).
--
-- When enabled (the default) a deploy that finds composer.json in the freshly
-- cloned release runs `composer install` as the site user before the build
-- command, so a Laravel/Symfony checkout is runnable without the operator
-- writing a build command at all. Turned off by operators who vendor their
-- dependencies or drive Composer from their own build command.
ALTER TABLE git_sources ADD COLUMN auto_composer INTEGER NOT NULL DEFAULT 1;
