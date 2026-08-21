import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
export function PreprocessingTimeline({ stages }) {
    return _jsx("ol", { className: "timeline", "aria-label": "Preprocessing stages", children: stages.map((stage, index) => _jsxs("li", { "data-status": stage.status, children: [_jsx("span", { "aria-hidden": "true", children: index + 1 }), _jsxs("div", { children: [_jsx("strong", { children: stage.name }), stage.detail === undefined ? null : _jsx("small", { children: stage.detail })] }), _jsx("em", { children: stage.status })] }, `${stage.name}-${index}`)) });
}
//# sourceMappingURL=PreprocessingTimeline.js.map