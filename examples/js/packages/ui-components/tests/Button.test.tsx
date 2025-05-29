import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { Button } from '../src/Button';

describe('Button Component', () => {
  test('renders with primary variant by default', () => {
    render(<Button>Click me</Button>);
    const button = screen.getByRole('button', { name: /click me/i });
    expect(button.className).toContain('bg-foreground');
  });

  test('renders with secondary variant when specified', () => {
    render(<Button variant="secondary">Click me</Button>);
    const button = screen.getByRole('button', { name: /click me/i });
    expect(button.className).toContain('hover:bg-[#f2f2f2]');
  });

  test('applies custom className when provided', () => {
    render(<Button className="custom-class">Click me</Button>);
    const button = screen.getByRole('button', { name: /click me/i });
    expect(button.className).toContain('custom-class');
  });

  test('calls onClick handler when clicked', () => {
    const handleClick = jest.fn();
    render(<Button onClick={handleClick}>Click me</Button>);
    const button = screen.getByRole('button', { name: /click me/i });
    fireEvent.click(button);
    expect(handleClick).toHaveBeenCalledTimes(1);
  });
});
