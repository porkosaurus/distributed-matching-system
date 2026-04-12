use crate::order::{Fill, Order, Side};
use crate::order_book::OrderBook;
use std::cmp::Reverse;

pub fn match_order(book: &mut OrderBook, mut incoming: Order) -> Vec<Fill> {
    let mut fills = Vec::new();

    match incoming.side {
        Side::Buy => {
            while incoming.quantity > 0 {
                let best_ask_price = match book.best_ask() {
                    Some(p) => p,
                    None => break,
                };

                if best_ask_price > incoming.price {
                    break;
                }

                let level = book.asks.get_mut(&best_ask_price).unwrap();
                let resting = level.front_mut().unwrap();

                let fill_qty = incoming.quantity.min(resting.quantity);

                fills.push(Fill {
                    buy_order_id: incoming.id,
                    sell_order_id: resting.id,
                    price: best_ask_price,
                    quantity: fill_qty,
                });

                resting.quantity -= fill_qty;
                incoming.quantity -= fill_qty;

                if resting.quantity == 0 {
                    level.pop_front();
                    if level.is_empty() {
                        book.asks.remove(&best_ask_price);
                    }
                }
            }

            // ADD THIS — remainder rests on the book
            if incoming.quantity > 0 {
                book.add_order(incoming);
            }
        }

        Side::Sell => {
            // incoming sell — match against bids (buyers)
            // a match happens when best bid price >= incoming sell price
            while incoming.quantity > 0 {
                let best_bid_price = match book.best_bid() {
                    Some(p) => p,
                    None => break,
                };

                if best_bid_price < incoming.price {
                    break; // no match — highest buyer won't pay what seller wants
                }

                let level = book.bids.get_mut(&Reverse(best_bid_price)).unwrap();
                let resting = level.front_mut().unwrap();

                let fill_qty = incoming.quantity.min(resting.quantity);

                fills.push(Fill {
                    buy_order_id: resting.id,
                    sell_order_id: incoming.id,
                    price: best_bid_price,
                    quantity: fill_qty,
                });

                resting.quantity -= fill_qty;
                incoming.quantity -= fill_qty;

                if resting.quantity == 0 {
                    level.pop_front();
                    if level.is_empty() {
                        book.bids.remove(&Reverse(best_bid_price));
                    }
                }
            }

            // whatever's left over rests on the book
            if incoming.quantity > 0 {
                book.add_order(incoming);
            }
        }
    }

    // for buy orders, add remainder to book after matching
    // we handle this outside the match since we moved incoming
    fills
}


#[cfg(test)]
mod tests {
    use super::*;
    use crate::order::{Order, Side};
    use crate::order_book::OrderBook;

    fn make_buy(id: u64, price: u64, qty: u64) -> Order {
        Order::new(id, Side::Buy, price, qty, id)
    }

    fn make_sell(id: u64, price: u64, qty: u64) -> Order {
        Order::new(id, Side::Sell, price, qty, id)
    }

    #[test]
    fn test_full_match() {
        let mut book = OrderBook::new();
        book.add_order(make_sell(1, 10000, 100));

        let fills = match_order(&mut book, make_buy(2, 10000, 100));

        assert_eq!(fills.len(), 1);
        assert_eq!(fills[0].quantity, 100);
        assert_eq!(fills[0].price, 10000);
        // book should be empty — both orders fully consumed
        assert!(book.best_ask().is_none());
        assert!(book.best_bid().is_none());
    }

    #[test]
    fn test_partial_fill_incoming_smaller() {
        let mut book = OrderBook::new();
        book.add_order(make_sell(1, 10000, 100)); // resting sell: 100 shares

        let fills = match_order(&mut book, make_buy(2, 10000, 60)); // buy only 60

        assert_eq!(fills.len(), 1);
        assert_eq!(fills[0].quantity, 60);
        // 40 shares should still be resting on the ask side
        assert_eq!(book.best_ask(), Some(10000));
    }

    #[test]
    fn test_partial_fill_incoming_larger() {
        let mut book = OrderBook::new();
        book.add_order(make_sell(1, 10000, 60)); // resting sell: only 60 shares

        let fills = match_order(&mut book, make_buy(2, 10000, 100)); // buy wants 100

        assert_eq!(fills.len(), 1);
        assert_eq!(fills[0].quantity, 60);
        // resting sell fully consumed, ask side empty
        assert!(book.best_ask().is_none());
    }

    #[test]
    fn test_no_match_prices_dont_cross() {
        let mut book = OrderBook::new();
        book.add_order(make_sell(1, 10100, 100)); // seller wants $101.00

        let fills = match_order(&mut book, make_buy(2, 10000, 100)); // buyer only pays $100.00

        assert!(fills.is_empty());
        // both orders should be on the book
        assert_eq!(book.best_ask(), Some(10100));
        assert_eq!(book.best_bid(), Some(10000));
    }

    #[test]
    fn test_cancel_mid_queue() {
        let mut book = OrderBook::new();
        book.add_order(make_buy(1, 10000, 100));
        book.add_order(make_buy(2, 10000, 200)); // this one gets cancelled
        book.add_order(make_buy(3, 10000, 300));

        let cancelled = book.cancel_order(2, &Side::Buy, 10000);
        assert!(cancelled);

        // price level still exists with orders 1 and 3
        let level = book.bids.get(&std::cmp::Reverse(10000)).unwrap();
        assert_eq!(level.len(), 2);
        assert_eq!(level[0].id, 1);
        assert_eq!(level[1].id, 3);
    }

    #[test]
    fn test_cancel_removes_empty_level() {
        let mut book = OrderBook::new();
        book.add_order(make_sell(1, 10000, 100)); // only order at this level

        book.cancel_order(1, &Side::Sell, 10000);

        // price level should be gone entirely
        assert!(book.best_ask().is_none());
    }

    #[test]
    fn test_time_priority_within_price_level() {
        let mut book = OrderBook::new();
        book.add_order(make_sell(1, 10000, 50));  // arrived first
        book.add_order(make_sell(2, 10000, 100)); // arrived second

        // buy 50 shares — should hit order 1 first (time priority)
        let fills = match_order(&mut book, make_buy(3, 10000, 50));

        assert_eq!(fills.len(), 1);
        assert_eq!(fills[0].sell_order_id, 1); // order 1 filled, not order 2
    }

    #[test]
    fn test_multiple_fills_from_one_order() {
        let mut book = OrderBook::new();
        book.add_order(make_sell(1, 10000, 30));
        book.add_order(make_sell(2, 10000, 30));
        book.add_order(make_sell(3, 10000, 30));

        // one big buy sweeps all three resting sells
        let fills = match_order(&mut book, make_buy(4, 10000, 90));

        assert_eq!(fills.len(), 3);
        assert_eq!(fills[0].sell_order_id, 1);
        assert_eq!(fills[1].sell_order_id, 2);
        assert_eq!(fills[2].sell_order_id, 3);
        assert!(book.best_ask().is_none());
    }
}