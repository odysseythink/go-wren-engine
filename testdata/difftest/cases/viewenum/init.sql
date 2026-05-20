CREATE TABLE orders (
    o_orderkey    INTEGER,
    o_custkey     INTEGER,
    o_totalprice  DOUBLE,
    o_orderdate   DATE,
    o_orderstatus VARCHAR
);
INSERT INTO orders VALUES
    (1, 100, 99.50, '2024-01-15', 'O'),
    (2, 200, 150.00, '2024-02-20', 'F'),
    (3, 100, 75.25, '2024-03-10', 'P');
