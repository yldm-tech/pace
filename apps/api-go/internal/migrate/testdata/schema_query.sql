-- The schema, in a form two databases can be compared by.
--
-- pg_dump would do, and is not used: its output carries the order objects happen to sit in the catalogue, which differs between a database built one migration at a time and one built the same way on another day. What is asked for here is every column, constraint and index, sorted, so the only thing a difference can mean is that the schemas differ.
--
-- Run against both databases and diff the two outputs.
SELECT 'column' AS kind,
       table_name || '.' || column_name || ' ' || data_type
         || coalesce(' (' || character_maximum_length || ')', '')
         || coalesce(' numeric(' || numeric_precision || ',' || numeric_scale || ')', '')
         || ' null=' || is_nullable
         || coalesce(' default=' || column_default, '') AS detail
  FROM information_schema.columns
 WHERE table_schema = 'public'
UNION ALL
SELECT 'constraint',
       conrelid::regclass || ' ' || conname || ' ' || pg_get_constraintdef(oid)
  FROM pg_constraint
 WHERE connamespace = 'public'::regnamespace
UNION ALL
SELECT 'index', schemaname || '.' || indexname || ' ' || indexdef
  FROM pg_indexes
 WHERE schemaname = 'public'
UNION ALL
SELECT 'sequence', sequence_name || ' ' || data_type
  FROM information_schema.sequences
 WHERE sequence_schema = 'public'
ORDER BY 1, 2;
