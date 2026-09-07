-- A section says how to write in it. See migrations/030_section_instruction.sql
-- for the reasoning; this is the same column for PostgreSQL.
ALTER TABLE "sections" ADD COLUMN "instruction" TEXT NOT NULL DEFAULT '';
