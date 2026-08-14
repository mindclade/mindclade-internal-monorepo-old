use mindclade_bounded_parse::{Cursor,Limits};
#[test] fn lines_are_bounded_and_located(){ let mut c=Cursor::new(b"a
b
",Limits::default()).expect("cursor"); let (loc,line)=c.next_line().expect("read").expect("line"); assert_eq!(loc.offset,0); assert_eq!(line,b"a"); }
