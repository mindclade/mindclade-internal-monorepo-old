use mindclade_bounded_parse::Limits;
#[test] fn default_nesting_is_bounded(){assert!(Limits::default().maximum_nesting>0);assert!(Limits::default().maximum_nesting<=128);}
