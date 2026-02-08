import '@testing-library/jest-dom';

// jsdom doesn't implement scrollTo
Element.prototype.scrollTo = function () {};
