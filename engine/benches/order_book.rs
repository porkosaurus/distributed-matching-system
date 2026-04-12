use criterion::{black_box, criterion_group, criterion_main, BenchmarkId, Criterion};
use engine::matching::match_order;
use engine::order::{Order, Side};
use engine::order_book::OrderBook;

// benchmark a single full match — the hot path
fn bench_single_match(c: &mut Criterion) {
    c.bench_function("single full match", |b| {
        b.iter(|| {
            let mut book = OrderBook::new();
            book.add_order(Order::new(1, Side::Sell, 10000, 100, 1));
            match_order(
                &mut book,
                black_box(Order::new(2, Side::Buy, 10000, 100, 2)),
            );
        })
    });
}

// benchmark matching against a book with varying depth
// this shows how latency scales with book size
fn bench_book_depth(c: &mut Criterion) {
    let mut group = c.benchmark_group("match vs book depth");

    for depth in [10, 100, 500, 1000].iter() {
        group.bench_with_input(
            BenchmarkId::from_parameter(depth),
            depth,
            |b, &depth| {
                b.iter(|| {
                    let mut book = OrderBook::new();
                    // fill book with resting sells at increasing prices
                    for i in 0..depth as u64 {
                        book.add_order(Order::new(i, Side::Sell, 10000 + i, 100, i));
                    }
                    // aggressive buy hits only the best ask
                    match_order(
                        &mut book,
                        black_box(Order::new(9999, Side::Buy, 10000, 100, 9999)),
                    );
                })
            },
        );
    }
    group.finish();
}

// benchmark add_order in isolation
fn bench_add_order(c: &mut Criterion) {
    c.bench_function("add_order", |b| {
        let mut book = OrderBook::new();
        let mut id = 0u64;
        b.iter(|| {
            id += 1;
            book.add_order(black_box(Order::new(id, Side::Buy, 10000, 100, id)));
        })
    });
}

criterion_group!(benches, bench_single_match, bench_book_depth, bench_add_order);
criterion_main!(benches);