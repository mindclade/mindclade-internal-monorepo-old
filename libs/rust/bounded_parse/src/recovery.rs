use crate::{
    Diagnostic, ParseMode
};

#[derive(Clone, Debug)] pub struct Recovery {
    mode: ParseMode, diagnostics: Vec<Diagnostic>
}

impl Recovery {
    #[must_use]pub fn new(mode: ParseMode) -> Self {
        Self {
            mode, diagnostics: Vec::new()
        }
    }
    #[must_use]pub const fn mode(&self) -> ParseMode {
        self.mode
    }
    pub fn record(&mut self, d: Diagnostic) {
        if self.mode==ParseMode::Recovery {
            self.diagnostics.push(d);
        }
    }
    #[must_use]pub fn diagnostics(&self) -> &[Diagnostic] {
        &self.diagnostics
    }
    pub fn into_diagnostics(self) -> Vec<Diagnostic> {
        self.diagnostics
    }
}
