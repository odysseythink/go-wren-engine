package io.wren.tools;

import io.trino.sql.SqlFormatter;
import io.trino.sql.parser.ParsingOptions;
import io.trino.sql.parser.SqlParser;
import io.trino.sql.tree.Statement;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.stream.Stream;

/**
 * FormatOracle freezes the Java reference output for P2's differential test.
 * For every cases/**.sql it runs SqlFormatter.formatSql(parseSql(sql)) — the
 * exact pair WrenPlanner uses — and writes the bytes to golden/**.sql.golden.
 * On a parse/format error it writes golden/**.sql.golden.error instead.
 */
public final class FormatOracle
{
    // Mirror io.wren.base.sqlrewrite.Utils: SqlParser with AS_DOUBLE.
    private static final SqlParser SQL_PARSER = new SqlParser();
    private static final ParsingOptions PARSING_OPTIONS =
            new ParsingOptions(ParsingOptions.DecimalLiteralTreatment.AS_DOUBLE);

    private FormatOracle() {}

    public static void main(String[] args)
            throws IOException
    {
        Path casesDir = Path.of(args.length > 0 ? args[0] : "testdata/format/cases");
        Path goldenDir = Path.of(args.length > 1 ? args[1] : "testdata/format/golden");

        List<Path> sqlFiles = new ArrayList<>();
        try (Stream<Path> walk = Files.walk(casesDir)) {
            walk.filter(p -> p.toString().endsWith(".sql")).sorted().forEach(sqlFiles::add);
        }

        int ok = 0;
        int errs = 0;
        for (Path sqlFile : sqlFiles) {
            String sql = Files.readString(sqlFile, StandardCharsets.UTF_8);
            Path rel = casesDir.relativize(sqlFile);
            Path goldenBase = goldenDir.resolve(rel.toString() + ".golden");
            Files.createDirectories(goldenBase.getParent());
            try {
                Statement stmt = SQL_PARSER.createStatement(sql, PARSING_OPTIONS);
                String formatted = SqlFormatter.formatSql(stmt);
                Files.writeString(goldenBase, formatted, StandardCharsets.UTF_8);
                Files.deleteIfExists(Path.of(goldenBase + ".error"));
                ok++;
                System.out.println("OK    " + rel);
            }
            catch (RuntimeException e) {
                Files.writeString(Path.of(goldenBase + ".error"),
                        e.getClass().getName() + ": " + e.getMessage(), StandardCharsets.UTF_8);
                Files.deleteIfExists(goldenBase);
                errs++;
                System.out.println("ERROR " + rel + " — " + e.getMessage());
            }
        }
        System.out.printf("%ncaptured %d golden, %d parse/format errors, %d total%n",
                ok, errs, sqlFiles.size());
    }
}
