-- A section says how to write in it. See migrations/030_section_instruction.sql
-- for the reasoning; this is the same column for MySQL, whose TEXT columns take
-- a default only in parentheses.
ALTER TABLE `sections` ADD COLUMN `instruction` LONGTEXT NOT NULL DEFAULT ('');
