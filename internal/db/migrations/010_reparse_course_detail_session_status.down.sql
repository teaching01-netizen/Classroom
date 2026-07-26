-- Forward-only semantic repair. This no-op only satisfies migration tooling;
-- operators must not run a pre-010 scraper after applying the new parser.
SELECT 1;
